package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionPriorityBillingFallbackIdempotencyAndReset(t *testing.T) {
	db := setupSubscriptionRefundTestDB(t)
	first := seedSubscriptionRefundFixture(t, db, 1000)
	second := first
	second.Id = 2
	second.EndTime += 86400
	require.NoError(t, db.Create(&second).Error)

	// Default remains earliest expiry. Saving an order changes no entitlement.
	var before []UserSubscription
	require.NoError(t, db.Order("id").Find(&before).Error)
	require.NoError(t, SetSubscriptionConsumptionPriority(first.UserId, []int{2, 1}))
	var after []UserSubscription
	require.NoError(t, db.Order("id").Find(&after).Error)
	require.Equal(t, before, after)
	// A stale quota snapshot cannot overwrite the independent preference.
	require.NoError(t, db.Save(&first).Error)
	stored, err := GetSubscriptionConsumptionPriority(first.UserId)
	require.NoError(t, err)
	require.Equal(t, []int{2, 1}, stored)
	active, err := GetAllActiveUserSubscriptions(first.UserId)
	require.NoError(t, err)
	require.Equal(t, 2, active[0].Subscription.Id)

	result, err := PreConsumeUserSubscription("priority-first", first.UserId, "gpt-6.2-sol", 0, 700)
	require.NoError(t, err)
	require.Equal(t, 2, result.UserSubscriptionId)
	result, err = PreConsumeUserSubscription("priority-fallback", first.UserId, "gpt-6.2-sol", 0, 500)
	require.NoError(t, err)
	require.Equal(t, 1, result.UserSubscriptionId)

	// Changing preference never rebinds or charges an already accepted request.
	require.NoError(t, SetSubscriptionConsumptionPriority(first.UserId, []int{1, 2}))
	result, err = PreConsumeUserSubscription("priority-first", first.UserId, "gpt-6.2-sol", 0, 700)
	require.NoError(t, err)
	require.Equal(t, 2, result.UserSubscriptionId)
	require.NoError(t, RefundSubscriptionPreConsume("priority-first"))
	require.NoError(t, RefundSubscriptionPreConsume("priority-first"))
	require.NoError(t, db.First(&second, 2).Error)
	require.Zero(t, second.AmountUsed)

	require.NoError(t, SetSubscriptionConsumptionPriority(first.UserId, []int{}))
	priority, err := GetSubscriptionConsumptionPriority(first.UserId)
	require.NoError(t, err)
	require.Empty(t, priority)
	result, err = PreConsumeUserSubscription("priority-reset", first.UserId, "gpt-6.2-sol", 0, 100)
	require.NoError(t, err)
	require.Equal(t, 1, result.UserSubscriptionId)
}

func TestSubscriptionPriorityRejectsForeignExpiredCancelledAndDuplicate(t *testing.T) {
	db := setupSubscriptionRefundTestDB(t)
	first := seedSubscriptionRefundFixture(t, db, 1000)
	for id, change := range map[int]func(*UserSubscription){
		2: func(s *UserSubscription) { s.UserId++ },
		3: func(s *UserSubscription) { s.EndTime = common.GetTimestamp() - 1 },
		4: func(s *UserSubscription) { s.Status = "cancelled" },
	} {
		sub := first
		sub.Id = id
		change(&sub)
		require.NoError(t, db.Create(&sub).Error)
	}
	require.NoError(t, SetSubscriptionConsumptionPriority(first.UserId, []int{1}))
	for _, ids := range [][]int{{2}, {3}, {4}, {9999}, {0}, {-1}, {1, 1}, {1, 2}, make([]int, 1001)} {
		require.ErrorIs(t, SetSubscriptionConsumptionPriority(first.UserId, ids), ErrSubscriptionPriorityInvalid)
		stored, err := GetSubscriptionConsumptionPriority(first.UserId)
		require.NoError(t, err)
		require.Equal(t, []int{1}, stored)
	}
	require.ErrorIs(t, SetSubscriptionConsumptionPriority(0, []int{}), ErrSubscriptionPriorityInvalid)
	// No connection outside the transaction: this must also work with one slot.
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	require.NoError(t, SetSubscriptionConsumptionPriority(first.UserId, []int{}))
}

func TestSubscriptionPriorityExpiredChoicesAndNewSubscriptions(t *testing.T) {
	db := setupSubscriptionRefundTestDB(t)
	first := seedSubscriptionRefundFixture(t, db, 1000)
	second := first
	second.Id = 2
	second.EndTime += 86400
	require.NoError(t, db.Create(&second).Error)
	require.NoError(t, SetSubscriptionConsumptionPriority(first.UserId, []int{2}))
	third := first
	third.Id = 3
	third.EndTime -= 3600
	require.NoError(t, db.Create(&third).Error)
	active, err := GetAllActiveUserSubscriptions(first.UserId)
	require.NoError(t, err)
	require.Equal(t, []int{2, 3, 1}, []int{active[0].Subscription.Id, active[1].Subscription.Id, active[2].Subscription.Id})
	require.NoError(t, db.Model(&UserSubscription{}).Where("id = ?", 2).Update("end_time", common.GetTimestamp()-1).Error)
	result, err := PreConsumeUserSubscription("priority-expired", first.UserId, "gpt-6.2-sol", 0, 10)
	require.NoError(t, err)
	require.Equal(t, 3, result.UserSubscriptionId)
}
