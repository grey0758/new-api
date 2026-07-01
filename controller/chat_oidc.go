package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

const (
	chatOIDCIssuer              = "https://api.open-codex.com/api/chat-oidc"
	chatOIDCDefaultClientID     = "opencodex-matrix-synapse"
	chatOIDCDefaultRedirectURI  = "https://matrix.open-codex.com/_synapse/client/oidc/callback"
	chatOIDCDefaultPostLoginURL = "https://api.open-codex.com/api/chat-oidc/authorize"
	chatOIDCKeyID               = "opencodex-chat-oidc-rs256"
)

var (
	chatOIDCState = newChatOIDCStore()
)

type chatOIDCStore struct {
	mutex        sync.Mutex
	authCodes    map[string]chatOIDCCode
	accessTokens map[string]chatOIDCAccessToken
}

type chatOIDCCode struct {
	UserID      int
	ClientID    string
	RedirectURI string
	Scope       string
	Nonce       string
	ExpiresAt   time.Time
}

type chatOIDCAccessToken struct {
	UserID    int
	ExpiresAt time.Time
}

type chatOIDCClaims struct {
	PreferredUsername string `json:"preferred_username,omitempty"`
	Name              string `json:"name,omitempty"`
	Email             string `json:"email,omitempty"`
	Group             string `json:"group,omitempty"`
	Role              int    `json:"role,omitempty"`
	Nonce             string `json:"nonce,omitempty"`
	jwt.RegisteredClaims
}

func newChatOIDCStore() *chatOIDCStore {
	return &chatOIDCStore{
		authCodes:    map[string]chatOIDCCode{},
		accessTokens: map[string]chatOIDCAccessToken{},
	}
}

func ChatOIDCDiscovery(c *gin.Context) {
	if !chatOIDCEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat_oidc_disabled"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"issuer":                                chatOIDCIssuer,
		"authorization_endpoint":                chatOIDCIssuer + "/authorize",
		"token_endpoint":                        chatOIDCIssuer + "/token",
		"userinfo_endpoint":                     chatOIDCIssuer + "/userinfo",
		"jwks_uri":                              chatOIDCIssuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"claims_supported": []string{
			"sub",
			"preferred_username",
			"name",
			"email",
			"group",
			"role",
		},
	})
}

func ChatOIDCJWKS(c *gin.Context) {
	if !chatOIDCEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat_oidc_disabled"})
		return
	}
	key, err := chatOIDCPrivateKey()
	if err != nil {
		common.SysError("chat oidc jwks key error: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"keys": []gin.H{
			{
				"kty": "RSA",
				"use": "sig",
				"kid": chatOIDCKeyID,
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
			},
		},
	})
}

func ChatOIDCAuthorize(c *gin.Context) {
	if !chatOIDCEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat_oidc_disabled"})
		return
	}
	if c.Query("response_type") != "code" {
		chatOIDCAuthorizeError(c, http.StatusBadRequest, c.Query("redirect_uri"), c.Query("state"), "unsupported_response_type", "only code response_type is supported")
		return
	}
	clientID := c.Query("client_id")
	redirectURI := c.Query("redirect_uri")
	if clientID != chatOIDCClientID() || redirectURI != chatOIDCRedirectURI() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	if !chatOIDCHasOpenIDScope(c.Query("scope")) {
		chatOIDCAuthorizeError(c, http.StatusBadRequest, redirectURI, c.Query("state"), "invalid_scope", "openid scope is required")
		return
	}

	user, ok, err := chatOIDCSessionUser(c)
	if err != nil {
		common.SysError("chat oidc session user error: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if !ok {
		c.Redirect(http.StatusFound, chatOIDCLoginURL(c))
		return
	}

	code, err := randomURLToken(32)
	if err != nil {
		common.SysError("chat oidc code generation error: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	chatOIDCState.storeCode(code, chatOIDCCode{
		UserID:      user.Id,
		ClientID:    clientID,
		RedirectURI: redirectURI,
		Scope:       c.Query("scope"),
		Nonce:       c.Query("nonce"),
		ExpiresAt:   time.Now().Add(2 * time.Minute),
	})

	values := url.Values{}
	values.Set("code", code)
	if state := c.Query("state"); state != "" {
		values.Set("state", state)
	}
	c.Redirect(http.StatusFound, redirectURI+"?"+values.Encode())
}

func ChatOIDCToken(c *gin.Context) {
	if !chatOIDCEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat_oidc_disabled"})
		return
	}
	if c.PostForm("grant_type") != "authorization_code" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_grant_type"})
		return
	}
	clientID, clientSecret, ok := c.Request.BasicAuth()
	if !ok {
		clientID = c.PostForm("client_id")
		clientSecret = c.PostForm("client_secret")
	}
	if clientID != chatOIDCClientID() || clientSecret == "" || clientSecret != os.Getenv("CHAT_OIDC_CLIENT_SECRET") {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_client"})
		return
	}
	codeValue := c.PostForm("code")
	code, ok := chatOIDCState.consumeCode(codeValue)
	if !ok || code.ClientID != clientID || code.RedirectURI != c.PostForm("redirect_uri") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}
	user, err := model.GetUserById(code.UserID, false)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysError("chat oidc token user lookup error: " + err.Error())
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}
	if user.Status == common.UserStatusDisabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}

	accessToken, err := randomURLToken(32)
	if err != nil {
		common.SysError("chat oidc access token generation error: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	chatOIDCState.storeAccessToken(accessToken, chatOIDCAccessToken{
		UserID:    user.Id,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	})

	idToken, err := chatOIDCIDToken(user, code.Nonce)
	if err != nil {
		common.SysError("chat oidc id token error: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   600,
		"id_token":     idToken,
		"scope":        code.Scope,
	})
}

func ChatOIDCUserInfo(c *gin.Context) {
	if !chatOIDCEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat_oidc_disabled"})
		return
	}
	token := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	accessToken, ok := chatOIDCState.getAccessToken(token)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
		return
	}
	user, err := model.GetUserById(accessToken.UserID, false)
	if err != nil || user.Status == common.UserStatusDisabled {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
		return
	}
	c.JSON(http.StatusOK, chatOIDCUserClaims(user))
}

func (s *chatOIDCStore) storeCode(code string, data chatOIDCCode) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.cleanupLocked(time.Now())
	s.authCodes[code] = data
}

func (s *chatOIDCStore) consumeCode(code string) (chatOIDCCode, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.cleanupLocked(time.Now())
	data, ok := s.authCodes[code]
	if ok {
		delete(s.authCodes, code)
	}
	return data, ok
}

func (s *chatOIDCStore) storeAccessToken(token string, data chatOIDCAccessToken) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.cleanupLocked(time.Now())
	s.accessTokens[token] = data
}

func (s *chatOIDCStore) getAccessToken(token string) (chatOIDCAccessToken, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.cleanupLocked(time.Now())
	data, ok := s.accessTokens[token]
	return data, ok
}

func (s *chatOIDCStore) cleanupLocked(now time.Time) {
	for code, data := range s.authCodes {
		if now.After(data.ExpiresAt) {
			delete(s.authCodes, code)
		}
	}
	for token, data := range s.accessTokens {
		if now.After(data.ExpiresAt) {
			delete(s.accessTokens, token)
		}
	}
}

func chatOIDCEnabled() bool {
	return strings.EqualFold(os.Getenv("CHAT_OIDC_ENABLED"), "true") &&
		strings.TrimSpace(os.Getenv("CHAT_OIDC_CLIENT_SECRET")) != ""
}

func chatOIDCClientID() string {
	if value := strings.TrimSpace(os.Getenv("CHAT_OIDC_CLIENT_ID")); value != "" {
		return value
	}
	return chatOIDCDefaultClientID
}

func chatOIDCRedirectURI() string {
	if value := strings.TrimSpace(os.Getenv("CHAT_OIDC_REDIRECT_URI")); value != "" {
		return value
	}
	return chatOIDCDefaultRedirectURI
}

func chatOIDCLoginURL(c *gin.Context) string {
	next := chatOIDCDefaultPostLoginURL + "?" + c.Request.URL.RawQuery
	values := url.Values{}
	values.Set("chat_oidc", "1")
	values.Set("next", next)
	return "https://api.open-codex.com/login?" + values.Encode()
}

func chatOIDCSessionUser(c *gin.Context) (*model.User, bool, error) {
	session := sessions.Default(c)
	rawID := session.Get("id")
	if rawID == nil {
		return nil, false, nil
	}
	userID, ok := rawID.(int)
	if !ok || userID == 0 {
		return nil, false, nil
	}
	user, err := model.GetUserById(userID, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if user.Status == common.UserStatusDisabled {
		return nil, false, nil
	}
	return user, true, nil
}

func chatOIDCHasOpenIDScope(scope string) bool {
	for _, part := range strings.Fields(scope) {
		if part == "openid" {
			return true
		}
	}
	return false
}

func chatOIDCAuthorizeError(c *gin.Context, status int, redirectURI string, state string, code string, description string) {
	if redirectURI == chatOIDCRedirectURI() {
		values := url.Values{}
		values.Set("error", code)
		values.Set("error_description", description)
		if state != "" {
			values.Set("state", state)
		}
		c.Redirect(http.StatusFound, redirectURI+"?"+values.Encode())
		return
	}
	c.JSON(status, gin.H{"error": code, "error_description": description})
}

func chatOIDCIDToken(user *model.User, nonce string) (string, error) {
	key, err := chatOIDCPrivateKey()
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims := chatOIDCClaims{
		PreferredUsername: chatOIDCSafeLocalpart(user.Username, user.Id),
		Name:              chatOIDCDisplayName(user),
		Email:             user.Email,
		Group:             user.Group,
		Role:              user.Role,
		Nonce:             nonce,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    chatOIDCIssuer,
			Subject:   chatOIDCSubject(user.Id),
			Audience:  jwt.ClaimStrings{chatOIDCClientID()},
			ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = chatOIDCKeyID
	return token.SignedString(key)
}

func chatOIDCUserClaims(user *model.User) gin.H {
	return gin.H{
		"sub":                chatOIDCSubject(user.Id),
		"preferred_username": chatOIDCSafeLocalpart(user.Username, user.Id),
		"name":               chatOIDCDisplayName(user),
		"email":              user.Email,
		"group":              user.Group,
		"role":               user.Role,
	}
}

func chatOIDCSubject(userID int) string {
	return fmt.Sprintf("newapi:%d", userID)
}

func chatOIDCDisplayName(user *model.User) string {
	if strings.TrimSpace(user.DisplayName) != "" {
		return user.DisplayName
	}
	return user.Username
}

func chatOIDCSafeLocalpart(username string, userID int) string {
	value := strings.ToLower(strings.TrimSpace(username))
	value = regexp.MustCompile(`[^a-z0-9._=-]+`).ReplaceAllString(value, "_")
	value = strings.Trim(value, "._=-")
	if value == "" {
		value = fmt.Sprintf("newapi_%d", userID)
	}
	if len(value) > 64 {
		value = value[:64]
	}
	return value
}

func chatOIDCPrivateKey() (*rsa.PrivateKey, error) {
	if raw := strings.TrimSpace(os.Getenv("CHAT_OIDC_RSA_PRIVATE_KEY")); raw != "" {
		return parseRSAPrivateKey([]byte(raw))
	}
	keyPath := strings.TrimSpace(os.Getenv("CHAT_OIDC_RSA_PRIVATE_KEY_FILE"))
	if keyPath == "" {
		keyPath = "/data/chat-oidc-rsa-private.pem"
	}
	data, err := os.ReadFile(keyPath)
	if err == nil {
		return parseRSAPrivateKey(data)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func parseRSAPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("missing private key PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return key, nil
}

func randomURLToken(byteLength int) (string, error) {
	buffer := make([]byte, byteLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	sum := sha256.Sum256(buffer)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
