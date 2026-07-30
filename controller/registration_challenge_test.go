package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type registrationChallengeAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Version     string `json:"version"`
		ChallengeID string `json:"challengeId"`
		Seed        string `json:"seed"`
		TargetHash  string `json:"targetHash"`
		Difficulty  int    `json:"difficulty"`
		ExpiresAt   int64  `json:"expiresAt"`
		ExpiresIn   int64  `json:"expiresIn"`
	} `json:"data"`
}

func registrationChallengeContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/user/register/challenge", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	return context, recorder
}

func TestCreateRegistrationChallenge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRegisterEnabled := common.RegisterEnabled
	originalPasswordRegisterEnabled := common.PasswordRegisterEnabled
	t.Cleanup(func() {
		common.RegisterEnabled = originalRegisterEnabled
		common.PasswordRegisterEnabled = originalPasswordRegisterEnabled
	})
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true

	context, recorder := registrationChallengeContext(`{"target":"Alice"}`)
	CreateRegistrationChallenge(context)

	var response registrationChallengeAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, "newapi-register-v1", response.Data.Version)
	require.Len(t, response.Data.ChallengeID, 22)
	require.Len(t, response.Data.Seed, 22)
	require.Len(t, response.Data.TargetHash, 64)
	require.Equal(t, 3, response.Data.Difficulty)
	require.Positive(t, response.Data.ExpiresAt)
	require.Equal(t, int64(120), response.Data.ExpiresIn)
}

func TestRegisterRejectsMissingRegistrationChallengeBeforeDatabaseAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRegisterEnabled := common.RegisterEnabled
	originalPasswordRegisterEnabled := common.PasswordRegisterEnabled
	originalEmailVerificationEnabled := common.EmailVerificationEnabled
	t.Cleanup(func() {
		common.RegisterEnabled = originalRegisterEnabled
		common.PasswordRegisterEnabled = originalPasswordRegisterEnabled
		common.EmailVerificationEnabled = originalEmailVerificationEnabled
	})
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	common.EmailVerificationEnabled = false

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/register",
		strings.NewReader(`{"username":"alice","password":"12345678"}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	Register(context)

	var response registrationChallengeAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.NotEmpty(t, response.Message)
}

func solveControllerRegistrationChallenge(t *testing.T, challenge registrationChallengeAPIResponse) string {
	t.Helper()
	requiredPrefix := strings.Repeat("0", challenge.Data.Difficulty)
	for nonce := uint64(0); ; nonce++ {
		material := fmt.Sprintf(
			"%s:%s:%s:%s:%d",
			challenge.Data.Version,
			challenge.Data.ChallengeID,
			challenge.Data.Seed,
			challenge.Data.TargetHash,
			nonce,
		)
		digest := sha256.Sum256([]byte(material))
		digestHex := hex.EncodeToString(digest[:])
		if strings.HasPrefix(digestHex, requiredPrefix) {
			return fmt.Sprintf(
				"%s.%s.%d.%s",
				challenge.Data.Version,
				challenge.Data.ChallengeID,
				nonce,
				digestHex,
			)
		}
	}
}

func TestRegisterConsumesValidChallengeAndRejectsReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalRedisEnabled := common.RedisEnabled
	originalRegisterEnabled := common.RegisterEnabled
	originalPasswordRegisterEnabled := common.PasswordRegisterEnabled
	originalEmailVerificationEnabled := common.EmailVerificationEnabled
	originalGenerateDefaultToken := constant.GenerateDefaultToken
	originalDefaultUseAutoGroup := setting.DefaultUseAutoGroup
	originalQuotaForNewUser := common.QuotaForNewUser
	originalQuotaForInviter := common.QuotaForInviter
	originalQuotaForInvitee := common.QuotaForInvitee
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		common.RegisterEnabled = originalRegisterEnabled
		common.PasswordRegisterEnabled = originalPasswordRegisterEnabled
		common.EmailVerificationEnabled = originalEmailVerificationEnabled
		constant.GenerateDefaultToken = originalGenerateDefaultToken
		setting.DefaultUseAutoGroup = originalDefaultUseAutoGroup
		common.QuotaForNewUser = originalQuotaForNewUser
		common.QuotaForInviter = originalQuotaForInviter
		common.QuotaForInvitee = originalQuotaForInvitee
	})

	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:registration_challenge_%d?mode=memory&cache=shared", time.Now().UnixNano())),
		&gorm.Config{},
	)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	model.DB = db
	common.RedisEnabled = false
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	common.EmailVerificationEnabled = false
	constant.GenerateDefaultToken = true
	setting.DefaultUseAutoGroup = false
	common.QuotaForNewUser = 0
	common.QuotaForInviter = 0
	common.QuotaForInvitee = 0

	challengeContext, challengeRecorder := registrationChallengeContext(`{"target":"alice"}`)
	CreateRegistrationChallenge(challengeContext)
	var challengeResponse registrationChallengeAPIResponse
	require.NoError(t, common.Unmarshal(challengeRecorder.Body.Bytes(), &challengeResponse))
	require.True(t, challengeResponse.Success)
	challengeToken := solveControllerRegistrationChallenge(t, challengeResponse)

	registerBody, err := common.Marshal(map[string]any{
		"username":       "alice",
		"password":       "12345678",
		"challengeToken": challengeToken,
	})
	require.NoError(t, err)
	registerRecorder := httptest.NewRecorder()
	registerContext, _ := gin.CreateTestContext(registerRecorder)
	registerContext.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(string(registerBody)))
	registerContext.Request.Header.Set("Content-Type", "application/json")
	Register(registerContext)

	var registerResponse registrationChallengeAPIResponse
	require.NoError(t, common.Unmarshal(registerRecorder.Body.Bytes(), &registerResponse))
	require.True(t, registerResponse.Success)

	var registeredUser model.User
	require.NoError(t, db.Where("username = ?", "alice").First(&registeredUser).Error)
	var initialToken model.Token
	require.NoError(t, db.Where("user_id = ?", registeredUser.Id).First(&initialToken).Error)
	require.Equal(t, "default", initialToken.Group)

	replayRecorder := httptest.NewRecorder()
	replayContext, _ := gin.CreateTestContext(replayRecorder)
	replayContext.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(string(registerBody)))
	replayContext.Request.Header.Set("Content-Type", "application/json")
	Register(replayContext)

	var replayResponse registrationChallengeAPIResponse
	require.NoError(t, common.Unmarshal(replayRecorder.Body.Bytes(), &replayResponse))
	require.False(t, replayResponse.Success)
	require.NotEmpty(t, replayResponse.Message)
}
