package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
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
	chatOIDCWebDefaultClientID  = "opencodex-web"
	chatOIDCWebDefaultRedirect  = "https://open-codex.com/api/account/sso/callback/"
	chatOIDCWebDefaultLogoutURL = "https://open-codex.com/"
	chatOIDCDefaultPostLoginURL = "https://api.open-codex.com/api/chat-oidc/authorize"
	chatOIDCKeyID               = "opencodex-chat-oidc-rs256"
)

var (
	chatOIDCState                = newChatOIDCStore()
	chatOIDCPKCEChallengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	chatOIDCPKCEVerifierPattern  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

type chatOIDCClient struct {
	ID           string
	RedirectURI  string
	Secret       string
	RequirePKCE  bool
	IssueSession bool
}

type chatOIDCStore struct {
	mutex        sync.Mutex
	authCodes    map[string]chatOIDCCode
	accessTokens map[string]chatOIDCAccessToken
}

type chatOIDCCode struct {
	UserID              int
	ClientID            string
	RedirectURI         string
	Scope               string
	Nonce               string
	CodeChallenge       string
	CodeChallengeMethod string
	ExpiresAt           time.Time
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
		"end_session_endpoint":                  chatOIDCIssuer + "/logout",
		"jwks_uri":                              chatOIDCIssuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post", "none"},
		"code_challenge_methods_supported":      []string{"S256"},
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
	client, ok := chatOIDCFindClient(clientID, redirectURI)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	codeChallenge := strings.TrimSpace(c.Query("code_challenge"))
	codeChallengeMethod := strings.TrimSpace(c.Query("code_challenge_method"))
	if client.RequirePKCE && (codeChallengeMethod != "S256" || !chatOIDCPKCEChallengePattern.MatchString(codeChallenge)) {
		chatOIDCAuthorizeError(c, http.StatusBadRequest, redirectURI, c.Query("state"), "invalid_request", "S256 PKCE is required")
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
		UserID:              user.Id,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scope:               c.Query("scope"),
		Nonce:               c.Query("nonce"),
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ExpiresAt:           time.Now().Add(2 * time.Minute),
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
	clientID, clientSecret, hasBasicAuth := c.Request.BasicAuth()
	if !hasBasicAuth {
		clientID = c.PostForm("client_id")
		clientSecret = c.PostForm("client_secret")
	}
	redirectURI := c.PostForm("redirect_uri")
	client, ok := chatOIDCFindClient(clientID, redirectURI)
	if !ok ||
		(client.Secret != "" && (clientSecret == "" || subtle.ConstantTimeCompare([]byte(clientSecret), []byte(client.Secret)) != 1)) ||
		(client.Secret == "" && clientSecret != "") {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_client"})
		return
	}
	codeValue := c.PostForm("code")
	codeVerifier := strings.TrimSpace(c.PostForm("code_verifier"))
	code, ok := chatOIDCState.consumeCode(codeValue, func(code chatOIDCCode) bool {
		if code.ClientID != clientID || code.RedirectURI != redirectURI {
			return false
		}
		if client.RequirePKCE {
			return chatOIDCVerifyPKCE(code.CodeChallenge, code.CodeChallengeMethod, codeVerifier)
		}
		return true
	})
	if !ok {
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
	if client.IssueSession {
		if err := chatOIDCSaveLoginSession(c, user); err != nil {
			common.SysError("chat oidc session save error: " + err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
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

	idToken, err := chatOIDCIDToken(user, code.Nonce, code.ClientID)
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

func ChatOIDCLogout(c *gin.Context) {
	if !chatOIDCEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat_oidc_disabled"})
		return
	}
	redirectURI := strings.TrimSpace(c.Query("post_logout_redirect_uri"))
	if redirectURI != chatOIDCWebLogoutRedirectURI() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	session := sessions.Default(c)
	session.Clear()
	if err := session.Save(); err != nil {
		common.SysError("chat oidc logout session error: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	c.Redirect(http.StatusFound, redirectURI)
}

func (s *chatOIDCStore) storeCode(code string, data chatOIDCCode) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.cleanupLocked(time.Now())
	s.authCodes[code] = data
}

func (s *chatOIDCStore) consumeCode(code string, validate func(chatOIDCCode) bool) (chatOIDCCode, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.cleanupLocked(time.Now())
	data, ok := s.authCodes[code]
	if !ok || (validate != nil && !validate(data)) {
		return chatOIDCCode{}, false
	}
	delete(s.authCodes, code)
	return data, true
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

func chatOIDCWebClientEnabled() bool {
	value := strings.TrimSpace(os.Getenv("OPENCODEX_WEB_OIDC_ENABLED"))
	return value == "" || strings.EqualFold(value, "true")
}

func chatOIDCWebClientID() string {
	if value := strings.TrimSpace(os.Getenv("OPENCODEX_WEB_OIDC_CLIENT_ID")); value != "" {
		return value
	}
	return chatOIDCWebDefaultClientID
}

func chatOIDCWebRedirectURI() string {
	if value := strings.TrimSpace(os.Getenv("OPENCODEX_WEB_OIDC_REDIRECT_URI")); value != "" {
		return value
	}
	return chatOIDCWebDefaultRedirect
}

func chatOIDCWebLogoutRedirectURI() string {
	if value := strings.TrimSpace(os.Getenv("OPENCODEX_WEB_OIDC_LOGOUT_REDIRECT_URI")); value != "" {
		return value
	}
	return chatOIDCWebDefaultLogoutURL
}

func chatOIDCClients() []chatOIDCClient {
	clients := []chatOIDCClient{
		{
			ID:          chatOIDCClientID(),
			RedirectURI: chatOIDCRedirectURI(),
			Secret:      os.Getenv("CHAT_OIDC_CLIENT_SECRET"),
		},
	}
	if chatOIDCWebClientEnabled() {
		clients = append(clients, chatOIDCClient{
			ID:           chatOIDCWebClientID(),
			RedirectURI:  chatOIDCWebRedirectURI(),
			RequirePKCE:  true,
			IssueSession: true,
		})
	}
	return clients
}

func chatOIDCFindClient(clientID string, redirectURI string) (chatOIDCClient, bool) {
	for _, client := range chatOIDCClients() {
		if client.ID == clientID && client.RedirectURI == redirectURI {
			return client, true
		}
	}
	return chatOIDCClient{}, false
}

func chatOIDCIsAllowedRedirectURI(redirectURI string) bool {
	for _, client := range chatOIDCClients() {
		if client.RedirectURI == redirectURI {
			return true
		}
	}
	return false
}

func chatOIDCLoginURL(c *gin.Context) string {
	next := chatOIDCDefaultPostLoginURL + "?" + c.Request.URL.RawQuery
	values := url.Values{}
	values.Set("chat_oidc", "1")
	values.Set("next", next)
	values.Set("sso_reauth", "1")
	loginPath := "/login"
	if strings.EqualFold(c.Query("screen"), "register") {
		loginPath = "/register"
	}
	return "https://api.open-codex.com" + loginPath + "?" + values.Encode()
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
	if chatOIDCIsAllowedRedirectURI(redirectURI) {
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

func chatOIDCIDToken(user *model.User, nonce string, clientID string) (string, error) {
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
			Audience:  jwt.ClaimStrings{clientID},
			ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = chatOIDCKeyID
	return token.SignedString(key)
}

func chatOIDCVerifyPKCE(challenge string, method string, verifier string) bool {
	if method != "S256" || !chatOIDCPKCEChallengePattern.MatchString(challenge) || !chatOIDCPKCEVerifierPattern.MatchString(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

func chatOIDCSaveLoginSession(c *gin.Context, user *model.User) error {
	session := sessions.Default(c)
	session.Set("id", user.Id)
	session.Set("username", user.Username)
	session.Set("role", user.Role)
	session.Set("status", user.Status)
	session.Set("group", user.Group)
	return session.Save()
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
