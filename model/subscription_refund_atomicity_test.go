package model

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSubscriptionRefundTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
		filepath.Join(t.TempDir(), "subscription-refund.db"),
	)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)

	require.NoError(t, db.AutoMigrate(
		&SubscriptionPlan{},
		&UserSubscription{},
		&SubscriptionPreConsumeRecord{},
	))

	previousDB := DB
	previousUsingSQLite := common.UsingSQLite
	DB = db
	common.UsingSQLite = true
	t.Cleanup(func() {
		DB = previousDB
		common.UsingSQLite = previousUsingSQLite
		_ = sqlDB.Close()
	})
	return db
}

func seedSubscriptionRefundFixture(t *testing.T, db *gorm.DB, amountTotal int64) UserSubscription {
	t.Helper()

	plan := SubscriptionPlan{
		Id:               1,
		Title:            "atomic-refund-plan",
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		Enabled:          true,
		TotalAmount:      amountTotal,
		QuotaResetPeriod: SubscriptionResetNever,
	}
	require.NoError(t, db.Create(&plan).Error)

	now := common.GetTimestamp()
	subscription := UserSubscription{
		Id:          1,
		UserId:      20,
		PlanId:      plan.Id,
		AmountTotal: amountTotal,
		StartTime:   now - 60,
		EndTime:     now + int64((24 * time.Hour).Seconds()),
		Status:      "active",
		Source:      "admin",
	}
	require.NoError(t, db.Create(&subscription).Error)
	return subscription
}

func TestRefundSubscriptionPreConsumeIsIdempotent(t *testing.T) {
	db := setupSubscriptionRefundTestDB(t)
	seedSubscriptionRefundFixture(t, db, 1_000_000)

	const requestID = "refund-idempotent"
	result, err := PreConsumeUserSubscription(requestID, 20, "gpt-image-2", 0, 30_000)
	require.NoError(t, err)
	require.EqualValues(t, 30_000, result.PreConsumed)

	require.NoError(t, RefundSubscriptionPreConsume(requestID))
	require.NoError(t, RefundSubscriptionPreConsume(requestID))

	var subscription UserSubscription
	require.NoError(t, db.First(&subscription, 1).Error)
	require.Zero(t, subscription.AmountUsed)

	var record SubscriptionPreConsumeRecord
	require.NoError(t, db.Where("request_id = ?", requestID).First(&record).Error)
	require.Equal(t, "refunded", record.Status)
}

func TestRefundSubscriptionPreConsumeRollsBackStatusWhenQuotaUpdateFails(t *testing.T) {
	db := setupSubscriptionRefundTestDB(t)

	record := SubscriptionPreConsumeRecord{
		RequestId:          "refund-missing-subscription",
		UserId:             20,
		UserSubscriptionId: 999,
		PreConsumed:        30_000,
		Status:             "consumed",
	}
	require.NoError(t, db.Create(&record).Error)

	require.Error(t, RefundSubscriptionPreConsume(record.RequestId))

	var stored SubscriptionPreConsumeRecord
	require.NoError(t, db.Where("request_id = ?", record.RequestId).First(&stored).Error)
	require.Equal(t, "consumed", stored.Status)
}

func TestConcurrentSubscriptionRefundsPreserveLedgerInvariant(t *testing.T) {
	db := setupSubscriptionRefundTestDB(t)
	seedSubscriptionRefundFixture(t, db, 10_000_000)

	const (
		requestCount = 24
		preConsumed  = int64(30_000)
	)
	for i := 0; i < requestCount; i++ {
		requestID := fmt.Sprintf("concurrent-image-%02d", i)
		_, err := PreConsumeUserSubscription(requestID, 20, "gpt-image-2", 0, preConsumed)
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, requestCount*2)
	for i := 0; i < requestCount; i++ {
		if i%3 != 0 {
			continue
		}
		requestID := fmt.Sprintf("concurrent-image-%02d", i)
		for duplicate := 0; duplicate < 2; duplicate++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errCh <- RefundSubscriptionPreConsume(requestID)
			}()
		}
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	var subscription UserSubscription
	require.NoError(t, db.First(&subscription, 1).Error)

	var consumedSum int64
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).
		Select("COALESCE(SUM(pre_consumed), 0)").
		Where("status = ?", "consumed").
		Scan(&consumedSum).Error)
	require.Equal(t, consumedSum, subscription.AmountUsed)

	var refundedCount int64
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).
		Where("status = ?", "refunded").
		Count(&refundedCount).Error)
	require.EqualValues(t, 8, refundedCount)
}
