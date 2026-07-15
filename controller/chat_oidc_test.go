package controller

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChatOIDCDiscoveryDisabled(t *testing.T) {
	t.Setenv("CHAT_OIDC_ENABLED", "")
	t.Setenv("CHAT_OIDC_CLIENT_SECRET", "")

	router := newChatOIDCTestRouter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chat-oidc/.well-known/openid-configuration", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestChatOIDCDiscovery(t *testing.T) {
	setupChatOIDCTestEnv(t)
	router := newChatOIDCTestRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chat-oidc/.well-known/openid-configuration", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, chatOIDCIssuer, body["issuer"])
	require.Equal(t, chatOIDCIssuer+"/authorize", body["authorization_endpoint"])
	require.Equal(t, chatOIDCIssuer+"/logout", body["end_session_endpoint"])
	require.Contains(t, body["code_challenge_methods_supported"], "S256")
	require.Contains(t, body["token_endpoint_auth_methods_supported"], "none")
}

func TestChatOIDCAuthorizeRequiresLogin(t *testing.T) {
	setupChatOIDCTestEnv(t)
	router := newChatOIDCTestRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chat-oidc/authorize?"+validAuthorizeQuery(), nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	location := rec.Header().Get("Location")
	require.Contains(t, location, "https://api.open-codex.com/login?")
	require.Contains(t, location, "chat_oidc=1")
	require.Contains(t, location, "sso_reauth=1")
	require.Contains(t, location, url.QueryEscape(chatOIDCIssuer+"/authorize"))
}

func TestChatOIDCWebAuthorizeRequiresPKCE(t *testing.T) {
	setupChatOIDCTestEnv(t)
	router := newChatOIDCTestRouter(t)
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", chatOIDCWebDefaultClientID)
	query.Set("redirect_uri", chatOIDCWebDefaultRedirect)
	query.Set("scope", "openid profile email")
	query.Set("state", "web-state")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chat-oidc/authorize?"+query.Encode(), nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	redirect, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, chatOIDCWebDefaultRedirect, redirect.Scheme+"://"+redirect.Host+redirect.Path)
	require.Equal(t, "invalid_request", redirect.Query().Get("error"))
	require.Equal(t, "web-state", redirect.Query().Get("state"))
}

func TestChatOIDCWebRegistrationEntry(t *testing.T) {
	setupChatOIDCTestEnv(t)
	router := newChatOIDCTestRouter(t)
	verifier := strings.Repeat("a", 64)
	query := validWebAuthorizeQuery(verifier)
	query.Set("screen", "register")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chat-oidc/authorize?"+query.Encode(), nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	location := rec.Header().Get("Location")
	require.Contains(t, location, "https://api.open-codex.com/register?")
	require.Contains(t, location, "chat_oidc=1")
	require.Contains(t, location, "sso_reauth=1")
}

func TestChatOIDCFullCodeFlow(t *testing.T) {
	setupChatOIDCTestEnv(t)
	setupChatOIDCTestDB(t)
	router := newChatOIDCTestRouter(t)

	cookieHeader := chatOIDCTestSessionCookie(t, router, 42)
	authRec := httptest.NewRecorder()
	authReq := httptest.NewRequest(http.MethodGet, "/api/chat-oidc/authorize?"+validAuthorizeQuery(), nil)
	authReq.Header.Set("Cookie", cookieHeader)
	router.ServeHTTP(authRec, authReq)

	require.Equal(t, http.StatusFound, authRec.Code)
	redirect, err := url.Parse(authRec.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, chatOIDCDefaultRedirectURI, redirect.Scheme+"://"+redirect.Host+redirect.Path)
	code := redirect.Query().Get("code")
	require.NotEmpty(t, code)
	require.Equal(t, "state-1", redirect.Query().Get("state"))

	tokenRec := httptest.NewRecorder()
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", chatOIDCDefaultRedirectURI)
	tokenReq := httptest.NewRequest(http.MethodPost, "/api/chat-oidc/token", strings.NewReader(form.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenReq.SetBasicAuth(chatOIDCDefaultClientID, "test-secret")
	router.ServeHTTP(tokenRec, tokenReq)

	require.Equal(t, http.StatusOK, tokenRec.Code)
	var tokenBody map[string]any
	require.NoError(t, json.Unmarshal(tokenRec.Body.Bytes(), &tokenBody))
	require.Equal(t, "Bearer", tokenBody["token_type"])
	require.NotEmpty(t, tokenBody["access_token"])
	require.NotEmpty(t, tokenBody["id_token"])

	reuseRec := httptest.NewRecorder()
	reuseReq := httptest.NewRequest(http.MethodPost, "/api/chat-oidc/token", strings.NewReader(form.Encode()))
	reuseReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reuseReq.SetBasicAuth(chatOIDCDefaultClientID, "test-secret")
	router.ServeHTTP(reuseRec, reuseReq)
	require.Equal(t, http.StatusBadRequest, reuseRec.Code)

	userInfoRec := httptest.NewRecorder()
	userInfoReq := httptest.NewRequest(http.MethodGet, "/api/chat-oidc/userinfo", nil)
	userInfoReq.Header.Set("Authorization", "Bearer "+tokenBody["access_token"].(string))
	router.ServeHTTP(userInfoRec, userInfoReq)

	require.Equal(t, http.StatusOK, userInfoRec.Code)
	var userInfo map[string]any
	require.NoError(t, json.Unmarshal(userInfoRec.Body.Bytes(), &userInfo))
	require.Equal(t, "newapi:42", userInfo["sub"])
	require.Equal(t, "matrix_user", userInfo["preferred_username"])
	require.Equal(t, "Matrix User", userInfo["name"])
	require.Equal(t, "matrix@example.com", userInfo["email"])
	require.Equal(t, "plus", userInfo["group"])
}

func TestChatOIDCWebPKCEFlowIssuesNewAPISession(t *testing.T) {
	setupChatOIDCTestEnv(t)
	setupChatOIDCTestDB(t)
	router := newChatOIDCTestRouter(t)
	verifier := strings.Repeat("b", 64)

	cookieHeader := chatOIDCTestSessionCookie(t, router, 42)
	authRec := httptest.NewRecorder()
	authReq := httptest.NewRequest(http.MethodGet, "/api/chat-oidc/authorize?"+validWebAuthorizeQuery(verifier).Encode(), nil)
	authReq.Header.Set("Cookie", cookieHeader)
	router.ServeHTTP(authRec, authReq)

	require.Equal(t, http.StatusFound, authRec.Code)
	redirect, err := url.Parse(authRec.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, chatOIDCWebDefaultRedirect, redirect.Scheme+"://"+redirect.Host+redirect.Path)
	code := redirect.Query().Get("code")
	require.NotEmpty(t, code)

	wrongVerifierRec := exchangeWebOIDCCode(router, code, strings.Repeat("c", 64))
	require.Equal(t, http.StatusBadRequest, wrongVerifierRec.Code)

	tokenRec := exchangeWebOIDCCode(router, code, verifier)
	require.Equal(t, http.StatusOK, tokenRec.Code)
	var tokenBody map[string]any
	require.NoError(t, json.Unmarshal(tokenRec.Body.Bytes(), &tokenBody))
	require.NotEmpty(t, tokenBody["access_token"])

	cookies := tokenRec.Result().Cookies()
	require.NotEmpty(t, cookies)
	sessionRec := httptest.NewRecorder()
	sessionReq := httptest.NewRequest(http.MethodGet, "/test-session", nil)
	sessionReq.AddCookie(cookies[0])
	router.ServeHTTP(sessionRec, sessionReq)
	require.Equal(t, http.StatusOK, sessionRec.Code)
	var sessionBody map[string]any
	require.NoError(t, json.Unmarshal(sessionRec.Body.Bytes(), &sessionBody))
	require.Equal(t, float64(42), sessionBody["id"])
}

func TestChatOIDCLogoutClearsSession(t *testing.T) {
	setupChatOIDCTestEnv(t)
	router := newChatOIDCTestRouter(t)
	cookieHeader := chatOIDCTestSessionCookie(t, router, 42)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/chat-oidc/logout?post_logout_redirect_uri="+url.QueryEscape(chatOIDCWebDefaultLogoutURL),
		nil,
	)
	req.Header.Set("Cookie", cookieHeader)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, chatOIDCWebDefaultLogoutURL, rec.Header().Get("Location"))
	require.NotEmpty(t, rec.Result().Cookies())
}

func TestChatOIDCAuthorizeRejectsWrongClient(t *testing.T) {
	setupChatOIDCTestEnv(t)
	router := newChatOIDCTestRouter(t)
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", "wrong")
	query.Set("redirect_uri", chatOIDCDefaultRedirectURI)
	query.Set("scope", "openid profile email")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/chat-oidc/authorize?"+query.Encode(), nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func setupChatOIDCTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CHAT_OIDC_ENABLED", "true")
	t.Setenv("CHAT_OIDC_CLIENT_SECRET", "test-secret")
	keyFile := t.TempDir() + "/chat-oidc-test.pem"
	t.Setenv("CHAT_OIDC_RSA_PRIVATE_KEY_FILE", keyFile)
	chatOIDCState = newChatOIDCStore()
}

func setupChatOIDCTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+url.QueryEscape(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(&model.User{
		Id:          42,
		Username:    "Matrix User!",
		DisplayName: "Matrix User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Email:       "matrix@example.com",
		Group:       "plus",
	}).Error)
}

func newChatOIDCTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	store := cookie.NewStore([]byte("test-session-secret"))
	store.Options(sessions.Options{Path: "/", MaxAge: 3600, HttpOnly: true})
	router.Use(sessions.Sessions("session", store))
	group := router.Group("/api/chat-oidc")
	group.GET("/.well-known/openid-configuration", ChatOIDCDiscovery)
	group.GET("/authorize", ChatOIDCAuthorize)
	group.POST("/token", ChatOIDCToken)
	group.GET("/userinfo", ChatOIDCUserInfo)
	group.GET("/logout", ChatOIDCLogout)
	group.GET("/jwks", ChatOIDCJWKS)
	router.GET("/test-session", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": sessions.Default(c).Get("id")})
	})
	return router
}

func chatOIDCTestSessionCookie(t *testing.T, router *gin.Engine, userID int) string {
	t.Helper()
	router.GET("/test-login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", userID)
		require.NoError(t, session.Save())
		c.String(http.StatusOK, "ok")
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test-login", nil)
	router.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies)
	return cookies[0].Name + "=" + cookies[0].Value
}

func validAuthorizeQuery() string {
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", chatOIDCDefaultClientID)
	query.Set("redirect_uri", chatOIDCDefaultRedirectURI)
	query.Set("scope", "openid profile email")
	query.Set("state", "state-1")
	query.Set("nonce", "nonce-1")
	return query.Encode()
}

func validWebAuthorizeQuery(verifier string) url.Values {
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", chatOIDCWebDefaultClientID)
	query.Set("redirect_uri", chatOIDCWebDefaultRedirect)
	query.Set("scope", "openid profile email")
	query.Set("state", "web-state")
	query.Set("nonce", "web-nonce")
	query.Set("code_challenge", chatOIDCTestPKCEChallenge(verifier))
	query.Set("code_challenge_method", "S256")
	return query
}

func exchangeWebOIDCCode(router *gin.Engine, code string, verifier string) *httptest.ResponseRecorder {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", chatOIDCWebDefaultClientID)
	form.Set("code", code)
	form.Set("redirect_uri", chatOIDCWebDefaultRedirect)
	form.Set("code_verifier", verifier)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/chat-oidc/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(rec, req)
	return rec
}

func chatOIDCTestPKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}
