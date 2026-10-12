package controller

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionAdminPreservesSettlementCurrency(t *testing.T) {
	db := setupRedemptionTokenTestDB(t)
	plan := model.SubscriptionPlan{
		Title: "VIP月卡", Currency: "CNY", PriceAmount: 300,
		DurationUnit: "day", DurationValue: 30, Enabled: true,
		TotalAmount: 25_000_000, QuotaResetPeriod: "daily",
	}
	c, recorder := newRedemptionTokenContext(t, "/api/subscription/admin/plans", map[string]any{"plan": plan}, "")
	AdminCreateSubscriptionPlan(c)
	var response redemptionTokenAPIResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	var stored model.SubscriptionPlan
	require.NoError(t, db.First(&stored).Error)
	require.Equal(t, "CNY", stored.Currency)
	require.EqualValues(t, 300, stored.PriceAmount)
	stored.PriceAmount = 600
	c, recorder = newRedemptionTokenContext(t, "/api/subscription/admin/plans/1", map[string]any{"plan": stored}, "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(stored.Id)}}
	AdminUpdateSubscriptionPlan(c)
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.NoError(t, db.First(&stored).Error)
	require.Equal(t, "CNY", stored.Currency)
	require.EqualValues(t, 600, stored.PriceAmount)
	stored.Currency = "EUR"
	c, recorder = newRedemptionTokenContext(t, "/api/subscription/admin/plans", map[string]any{"plan": stored}, "")
	AdminCreateSubscriptionPlan(c)
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
}

func TestSaleRetirementPreservesIssuedCodeButRejectsNewCheckout(t *testing.T) {
	db := setupRedemptionTokenTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionOrder{}))
	plan := model.SubscriptionPlan{Title: "old weekly monthly", Enabled: true,
		SaleRetiredAt: time.Now().Unix(), Currency: "USD", PriceAmount: 260,
		DurationUnit: "day", DurationValue: 28, TotalAmount: 175_000_000,
		QuotaResetPeriod: "custom", QuotaResetCustomSeconds: 604800}
	require.NoError(t, db.Create(&plan).Error)
	for _, public := range []bool{false, true} {
		plans, err := model.ListSubscriptionPlans(public)
		require.NoError(t, err)
		require.Empty(t, plans)
	}
	for _, checkout := range []gin.HandlerFunc{SubscriptionRequestEpay, SubscriptionRequestStripePay, SubscriptionRequestCreemPay} {
		c, recorder := newRedemptionTokenContext(t, "/checkout", map[string]any{"plan_id": plan.Id}, "")
		checkout(c)
		var response redemptionTokenAPIResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		require.False(t, response.Success)
		require.Equal(t, "套餐未启用", response.Message)
	}
	user := model.User{Id: 99, Username: "legacy-code-user", Status: 1, Group: "default"}
	require.NoError(t, db.Create(&user).Error)
	code := model.Redemption{Key: "isolated-sale-retirement-fixture", Status: common.RedemptionCodeStatusEnabled,
		Type: model.RedemptionTypeSubscription, SubscriptionPlanId: plan.Id}
	require.NoError(t, db.Create(&code).Error)
	result, err := model.Redeem(code.Key, user.Id)
	require.NoError(t, err)
	require.NotNil(t, result.Subscription)
	require.Equal(t, plan.Id, result.Subscription.PlanId)
	require.EqualValues(t, 175_000_000, result.Subscription.AmountTotal)
	model.InvalidateSubscriptionPlanCache(plan.Id)
}
