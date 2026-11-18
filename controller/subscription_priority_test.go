package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionPriorityAPIUsesAuthenticatedOwner(t *testing.T) {
	db := setupRedemptionTokenTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionConsumptionPreference{}))
	require.NoError(t, db.Create(&model.UserSubscription{Id: 301, UserId: 20, Status: "active", EndTime: time.Now().Unix() + 3600}).Error)
	require.NoError(t, db.Create(&model.UserSubscription{Id: 302, UserId: 21, Status: "active", EndTime: time.Now().Unix() + 3600}).Error)
	for _, tc := range []struct {
		body map[string]any
		ok   bool
	}{
		{map[string]any{"subscription_ids": []int{301}, "user_id": 21}, true},
		{map[string]any{"subscription_ids": []int{302}, "user_id": 21}, false},
		{map[string]any{"subscription_ids": []int{301, 301}}, false},
		{map[string]any{}, false},
		{map[string]any{"subscription_ids": nil}, false},
		{map[string]any{"subscription_ids": "301"}, false},
		{map[string]any{"subscription_ids": []int{}}, true},
	} {
		c, recorder := newRedemptionTokenContext(t, "/api/subscription/self/priority", tc.body, "")
		c.Set("id", 20)
		UpdateSubscriptionPriority(c)
		response := decodeRedemptionTokenResponse(t, recorder)
		require.Equal(t, tc.ok, response.Success, "%v: %s", tc.body, recorder.Body.String())
	}
	var count int64
	require.NoError(t, db.Model(&model.SubscriptionConsumptionPreference{}).Where("user_id = ?", 21).Count(&count).Error)
	require.Zero(t, count)
}
