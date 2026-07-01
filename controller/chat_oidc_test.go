package controller

import (
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
	require.Contains(t, location, url.QueryEscape(chatOIDCIssuer+"/authorize"))
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
	db, err := gorm.Open(sqlite.Open("file:chat_oidc_test?mode=memory&cache=shared"), &gorm.Config{})
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
	group.GET("/jwks", ChatOIDCJWKS)
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

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}
