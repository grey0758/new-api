package controller

import (
	"encoding/json"
	"fmt"
	"testing"

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
