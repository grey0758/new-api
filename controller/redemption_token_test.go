package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type redemptionTokenAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type redemptionTokenGenerateResponse struct {
	Keys []string `json:"keys"`
	Text string   `json:"text"`
}

func setupRedemptionTokenTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	model.DB = db
	model.LOG_DB = db

	if err := db.AutoMigrate(
		&model.Redemption{},
		&model.SubscriptionPlan{},
		&model.User{},
		&model.UserSubscription{},
		&model.Log{},
	); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func newRedemptionTokenContext(t *testing.T, target string, body any, token string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var requestBody *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(payload)
	} else {
		requestBody = bytes.NewReader(nil)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, target, requestBody)
	if body != nil {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		ctx.Request.Header.Set("Authorization", "Bearer "+token)
	}
	return ctx, recorder
}

func newRedemptionTokenGetContext(t *testing.T, target string, token string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		ctx.Request.Header.Set("Authorization", "Bearer "+token)
	}
	return ctx, recorder
}

func decodeRedemptionTokenResponse(t *testing.T, recorder *httptest.ResponseRecorder) redemptionTokenAPIResponse {
	t.Helper()

	var response redemptionTokenAPIResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode api response: %v\nbody=%s", err, recorder.Body.String())
	}
	return response
}

func seedRedemptionTokenPlan(t *testing.T, db *gorm.DB, planId int, enabled bool) model.SubscriptionPlan {
	t.Helper()
	return seedRedemptionTokenPlanWithTitle(t, db, planId, enabled, "20日卡")
}

func seedRedemptionTokenPlanWithTitle(t *testing.T, db *gorm.DB, planId int, enabled bool, title string) model.SubscriptionPlan {
	t.Helper()

	plan := model.SubscriptionPlan{
		Id:            planId,
		Title:         title,
		PriceAmount:   0,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationDay,
		DurationValue: 20,
		Enabled:       enabled,
		TotalAmount:   12345,
	}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatalf("failed to create subscription plan: %v", err)
	}
	model.InvalidateSubscriptionPlanCache(planId)
	return plan
}

func TestGenerateRedemptionWithTokenRequiresValidToken(t *testing.T) {
	setupRedemptionTokenTestDB(t)
	t.Setenv(redemptionGenerateTokenEnv, "expected-token")

	for _, tc := range []struct {
		name  string
		token string
	}{
		{name: "missing token"},
		{name: "invalid token", token: "wrong-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, recorder := newRedemptionTokenContext(t, "/api/redemption/generate-with-token", map[string]any{}, tc.token)
			GenerateRedemptionWithToken(ctx)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected unauthorized status, got %d body=%s", recorder.Code, recorder.Body.String())
			}
			response := decodeRedemptionTokenResponse(t, recorder)
			if response.Success {
				t.Fatalf("expected failure response")
			}
		})
	}
}

func TestGenerateRedemptionWithTokenUsesDefaultPlanAndFormatsShareText(t *testing.T) {
	db := setupRedemptionTokenTestDB(t)
	seedRedemptionTokenPlanWithTitle(t, db, 77, true, "5刀体验日卡")
	t.Setenv(redemptionGenerateTokenEnv, "expected-token")
	t.Setenv(redemptionGenerateDefaultPlanIDEnv, "77")

	oldServerAddress := system_setting.ServerAddress
	oldDocsLink := operation_setting.GetGeneralSetting().DocsLink
	system_setting.ServerAddress = "https://api.opencodex.uk/"
	operation_setting.GetGeneralSetting().DocsLink = "https://docs.opencodex.uk/opencodex/opencodex-uk/"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
		operation_setting.GetGeneralSetting().DocsLink = oldDocsLink
	})

	ctx, recorder := newRedemptionTokenContext(t, "/api/redemption/generate-with-token", map[string]any{
		"count": 2,
	}, "expected-token")
	GenerateRedemptionWithToken(ctx)

	response := decodeRedemptionTokenResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s body=%s", response.Message, recorder.Body.String())
	}

	var data redemptionTokenGenerateResponse
	if err := json.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("failed to decode generate response: %v", err)
	}
	if len(data.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(data.Keys))
	}

	expectedPrefix := "注册地址：https://api.opencodex.uk\n文档地址：https://docs.opencodex.uk/opencodex/opencodex-uk\n5刀体验日卡兑换码：\n"
	if !strings.HasPrefix(data.Text, expectedPrefix) {
		t.Fatalf("unexpected share text prefix:\n%s", data.Text)
	}
	for _, key := range data.Keys {
		if !strings.Contains(data.Text, key) {
			t.Fatalf("share text missing generated key %q: %s", key, data.Text)
		}
	}

	var redemptions []model.Redemption
	if err := db.Find(&redemptions).Error; err != nil {
		t.Fatalf("failed to load redemptions: %v", err)
	}
	if len(redemptions) != 2 {
		t.Fatalf("expected 2 redemption rows, got %d", len(redemptions))
	}
	for _, redemption := range redemptions {
		if redemption.Type != model.RedemptionTypeSubscription || redemption.SubscriptionPlanId != 77 || redemption.Name != "5刀体验日卡" {
			t.Fatalf("unexpected redemption row: %+v", redemption)
		}
	}
}

func TestGenerateRedemptionWithTokenReturnsBrowserPageForGet(t *testing.T) {
	db := setupRedemptionTokenTestDB(t)
	seedRedemptionTokenPlanWithTitle(t, db, 77, true, "5刀体验日卡")
	t.Setenv(redemptionGenerateTokenEnv, "expected-token")
	t.Setenv(redemptionGenerateDefaultPlanIDEnv, "77")

	ctx, recorder := newRedemptionTokenGetContext(t, "/api/redemption/generate-with-token?count=1", "expected-token")
	GenerateRedemptionWithToken(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "<title>兑换码生成结果</title>") {
		t.Fatalf("missing page title: %s", body)
	}
	if !strings.Contains(body, "复制文案") {
		t.Fatalf("missing copy button: %s", body)
	}
	if !strings.Contains(body, "注册地址：") || !strings.Contains(body, "5刀体验日卡兑换码：") {
		t.Fatalf("missing share text in html: %s", body)
	}
	if strings.Contains(body, "%!") {
		t.Fatalf("html contains fmt formatting error: %s", body)
	}
	if !strings.Contains(body, "注册地址：http://localhost:3000\n文档地址：") {
		t.Fatalf("share text did not preserve line breaks: %s", body)
	}
}

func TestGetRedemptionSubscriptionPlansWithToken(t *testing.T) {
	db := setupRedemptionTokenTestDB(t)
	seedRedemptionTokenPlanWithTitle(t, db, 77, true, "5刀体验日卡")
	seedRedemptionTokenPlan(t, db, 88, false)
	if err := db.Model(&model.SubscriptionPlan{}).Where("id = ?", 88).Update("enabled", false).Error; err != nil {
		t.Fatalf("failed to disable test plan: %v", err)
	}
	t.Setenv(redemptionGenerateTokenEnv, "expected-token")
	t.Setenv(redemptionGenerateDefaultPlanIDEnv, "77")

	ctx, recorder := newRedemptionTokenGetContext(t, "/api/redemption/subscription-plans-with-token", "expected-token")
	GetRedemptionSubscriptionPlansWithToken(ctx)

	response := decodeRedemptionTokenResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s body=%s", response.Message, recorder.Body.String())
	}
	var data struct {
		Plans []struct {
			Id    int    `json:"id"`
			Title string `json:"title"`
		} `json:"plans"`
		DefaultPlanId int    `json:"default_plan_id"`
		DefaultName   string `json:"default_name"`
	}
	if err := json.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("failed to decode plans response: %v", err)
	}
	if data.DefaultPlanId != 77 {
		t.Fatalf("expected default plan 77, got %d", data.DefaultPlanId)
	}
	if data.DefaultName != "5刀体验日卡" {
		t.Fatalf("expected default name from plan title, got %q", data.DefaultName)
	}
	if len(data.Plans) != 1 || data.Plans[0].Id != 77 || data.Plans[0].Title != "5刀体验日卡" {
		t.Fatalf("expected only enabled plan 77, got %+v", data.Plans)
	}
}

func TestTokenGeneratedSubscriptionRedemptionCanBeRedeemed(t *testing.T) {
	db := setupRedemptionTokenTestDB(t)
	plan := seedRedemptionTokenPlan(t, db, 88, true)
	t.Setenv(redemptionGenerateTokenEnv, "expected-token")

	user := model.User{
		Id:       3001,
		Username: "redeemer",
		Email:    "redeemer@example.com",
		Password: "placeholder",
		Group:    "plus",
		Status:   common.UserStatusEnabled,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	ctx, recorder := newRedemptionTokenContext(t, "/api/redemption/generate-with-token", map[string]any{
		"subscription_plan_id": plan.Id,
		"base_url":             "https://api.opencodex.uk",
		"docs_url":             "https://docs.opencodex.uk/opencodex/opencodex-uk",
	}, "expected-token")
	GenerateRedemptionWithToken(ctx)

	response := decodeRedemptionTokenResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected generate success, got message: %s body=%s", response.Message, recorder.Body.String())
	}
	var data redemptionTokenGenerateResponse
	if err := json.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("failed to decode generate response: %v", err)
	}
	if len(data.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(data.Keys))
	}

	result, err := model.Redeem(data.Keys[0], user.Id)
	if err != nil {
		t.Fatalf("expected redeem to succeed: %v", err)
	}
	if result.Type != model.RedemptionTypeSubscription || result.Subscription == nil {
		t.Fatalf("expected subscription redemption result, got %+v", result)
	}
	if result.Subscription.PlanId != plan.Id || result.Subscription.UserId != user.Id {
		t.Fatalf("unexpected subscription: %+v", result.Subscription)
	}
	if result.Subscription.EndTime <= result.Subscription.StartTime {
		t.Fatalf("expected subscription end time after start time: %+v", result.Subscription)
	}
}
