package controller

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func TestStudioPlanningUsage(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	if err := db.AutoMigrate(&model.User{}, &model.Log{}); err != nil {
		t.Fatal(err)
	}
	users := []model.User{
		{Id: 1, Username: "billing-worker", AffCode: "bill", Password: "unused", Group: "studio-planning", Role: common.RoleCommonUser, Quota: 123456},
		{Id: 2, Username: "someone-else", AffCode: "else", Password: "unused", Group: "default", Role: common.RoleCommonUser},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	logs := []model.Log{
		{Id: 10, UserId: 1, Type: model.LogTypeConsume, TokenId: 7, Quota: 70},
		{Id: 19, UserId: 2, Type: model.LogTypeConsume, TokenId: 8, Quota: 99999},
		{Id: 20, UserId: 1, Type: model.LogTypeConsume, TokenId: 7, Quota: 700, PromptTokens: 19, CompletionTokens: 4, ModelName: "sol", Content: "private text"},
		{Id: 29, UserId: 1, Type: model.LogTypeManage, Quota: 99999},
		{Id: 30, UserId: 1, Type: model.LogTypeRefund, TokenId: 7, Quota: 100},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/log/self/studio-planning?after_id=10", nil, 1)
	ctx.Set("use_access_token", true)
	GetStudioPlanningUsage(ctx)
	var statement struct {
		Success bool `json:"success"`
		Data    struct {
			Items        []studioPlanningCharge `json:"items"`
			BalanceQuota int                    `json:"balanceQuota"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &statement); err != nil {
		t.Fatal(err)
	}
	rows := statement.Data.Items
	if recorder.Code != http.StatusOK || !statement.Success || statement.Data.BalanceQuota != 123456 || len(rows) != 2 || strings.Contains(recorder.Body.String(), "private text") {
		t.Fatalf("unexpected statement: %s", recorder.Body.String())
	}
	if rows[0].ID != 20 || rows[0].TokenID != 7 || rows[0].Quota != 700 || rows[0].PromptTokens != 19 || rows[0].CompletionTokens != 4 || rows[1].ID != 30 || rows[1].Type != model.LogTypeRefund {
		t.Fatalf("invalid charge mapping: %+v", rows)
	}
	for _, check := range []struct {
		id          int
		accessToken bool
		query       string
		status      int
	}{{2, true, "", 403}, {1, false, "", 403}, {1, true, "?after_id=-1", 400}} {
		ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/log/self/studio-planning"+check.query, nil, check.id)
		ctx.Set("use_access_token", check.accessToken)
		GetStudioPlanningUsage(ctx)
		if recorder.Code != check.status {
			t.Fatalf("unexpected status: %d", recorder.Code)
		}
	}
}
