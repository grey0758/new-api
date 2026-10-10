package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestArchivedSubscriptionPlanPreservesExistingBilling(t *testing.T) {
	db := setupSubscriptionRefundTestDB(t)
	sub := seedSubscriptionRefundFixture(t, db, 1_000_000)
	require.NoError(t, db.Model(&SubscriptionPlan{}).Where("id = ?", sub.PlanId).
		Updates(map[string]interface{}{"enabled": false, "archived_at": common.GetTimestamp()}).Error)
	InvalidateSubscriptionPlanCache(sub.PlanId)
	t.Cleanup(func() { InvalidateSubscriptionPlanCache(sub.PlanId) })

	for _, enabledOnly := range []bool{false, true} {
		plans, err := ListSubscriptionPlans(enabledOnly)
		require.NoError(t, err)
		require.Empty(t, plans)
	}
	plan, err := GetSubscriptionPlanById(sub.PlanId)
	require.NoError(t, err)
	require.Positive(t, plan.ArchivedAt)

	result, err := PreConsumeUserSubscription("archived-existing-billing", sub.UserId, "gpt-image-2", 0, 30_000)
	require.NoError(t, err)
	require.Equal(t, sub.Id, result.UserSubscriptionId)
	require.EqualValues(t, 30_000, result.AmountUsedAfter)

	err = db.Transaction(func(tx *gorm.DB) error {
		_, err := CreateUserSubscriptionFromPlanTx(tx, 99, plan, "admin")
		return err
	})
	require.ErrorContains(t, err, "archived")
	var count int64
	require.NoError(t, db.Model(&UserSubscription{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, RefundSubscriptionPreConsume("archived-existing-billing"))
	require.NoError(t, db.First(&sub, sub.Id).Error)
	require.Zero(t, sub.AmountUsed)
}

func TestSubscriptionPlanCatalogSeparatesArchivedAndDisabled(t *testing.T) {
	db := setupSubscriptionRefundTestDB(t)
	for _, plan := range []SubscriptionPlan{
		{Id: 101, Title: "enabled", Enabled: true},
		{Id: 102, Title: "disabled", Enabled: false},
		{Id: 103, Title: "archived", Enabled: false, ArchivedAt: 1},
		{Id: 104, Title: "archived-enabled", Enabled: true, ArchivedAt: 1},
	} {
		enabled := plan.Enabled
		require.NoError(t, db.Create(&plan).Error)
		if !enabled {
			require.NoError(t, db.Model(&plan).Update("enabled", false).Error)
		}
	}
	adminPlans, err := ListSubscriptionPlans(false)
	require.NoError(t, err)
	require.Len(t, adminPlans, 2)
	publicPlans, err := ListSubscriptionPlans(true)
	require.NoError(t, err)
	require.Len(t, publicPlans, 1)
	require.Equal(t, 101, publicPlans[0].Id)
}

func TestFourCompleteSevenDaySubscriptionCycles(t *testing.T) {
	start := time.Date(2026, time.October, 10, 15, 30, 0, 0, time.UTC)
	plan := SubscriptionPlan{
		DurationUnit: SubscriptionDurationDay, DurationValue: 28,
		QuotaResetPeriod: SubscriptionResetCustom, QuotaResetCustomSeconds: 7 * 86400,
	}
	end, err := calcPlanEndTime(start, &plan)
	require.NoError(t, err)
	require.Equal(t, start.Add(28*24*time.Hour).Unix(), end)
	base := start
	for cycle := 1; cycle <= 4; cycle++ {
		next := calcNextResetTime(base, &plan, end)
		require.Equal(t, start.Add(time.Duration(cycle)*7*24*time.Hour).Unix(), next)
		base = time.Unix(next, 0)
	}
	require.Zero(t, calcNextResetTime(base, &plan, end))
}
