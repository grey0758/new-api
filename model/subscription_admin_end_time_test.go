package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedAdminSubscriptionEndTimeTest(t *testing.T, userId int, sub *UserSubscription) {
	t.Helper()
	initCol()
	user := &User{
		Id:       userId,
		Username: "subscription_end_time_user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, DB.Create(user).Error)
	sub.UserId = userId
	require.NoError(t, DB.Create(sub).Error)
}

func TestAddCalendarMonthsClamped(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	tests := []struct {
		name string
		base time.Time
		want time.Time
	}{
		{
			name: "same day next month",
			base: time.Date(2026, time.July, 16, 10, 20, 30, 0, location),
			want: time.Date(2026, time.August, 16, 10, 20, 30, 0, location),
		},
		{
			name: "clamp to february",
			base: time.Date(2026, time.January, 31, 10, 20, 30, 0, location),
			want: time.Date(2026, time.February, 28, 10, 20, 30, 0, location),
		},
		{
			name: "clamp to leap day",
			base: time.Date(2028, time.January, 31, 10, 20, 30, 0, location),
			want: time.Date(2028, time.February, 29, 10, 20, 30, 0, location),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := addCalendarMonthsClamped(test.base.Unix(), 1, "Asia/Shanghai")
			require.NoError(t, err)
			assert.Equal(t, test.want.Unix(), got)
		})
	}
}

func TestAdminUpdateUserSubscriptionEndTimeRenewMonth(t *testing.T) {
	truncateTables(t)
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	start := time.Date(2099, time.June, 16, 10, 20, 30, 0, location)
	end := time.Date(2099, time.July, 16, 10, 20, 30, 0, location)
	sub := &UserSubscription{
		Id:          101,
		PlanId:      7,
		AmountTotal: 1000,
		AmountUsed:  123,
		StartTime:   start.Unix(),
		EndTime:     end.Unix(),
		Status:      "active",
		Source:      "admin",
	}
	seedAdminSubscriptionEndTimeTest(t, 11, sub)

	updated, message, err := AdminUpdateUserSubscriptionEndTime(AdminUpdateUserSubscriptionEndTimeParams{
		UserSubscriptionId: sub.Id,
		UserId:             sub.UserId,
		ExpectedEndTime:    end.Unix(),
		Action:             AdminUserSubscriptionEndTimeActionRenewMonth,
		Timezone:           "Asia/Shanghai",
	})
	require.NoError(t, err)
	assert.Empty(t, message)
	assert.Equal(t, time.Date(2099, time.August, 16, 10, 20, 30, 0, location).Unix(), updated.EndTime)
	assert.Equal(t, int64(1000), updated.AmountTotal)
	assert.Equal(t, int64(123), updated.AmountUsed)
	assert.Equal(t, "active", updated.Status)
}

func TestAdminUpdateUserSubscriptionEndTimeRejectsStaleAndCancelled(t *testing.T) {
	truncateTables(t)
	now := time.Now()
	sub := &UserSubscription{
		Id:        102,
		PlanId:    7,
		StartTime: now.Add(-24 * time.Hour).Unix(),
		EndTime:   now.Add(24 * time.Hour).Unix(),
		Status:    "active",
		Source:    "admin",
	}
	seedAdminSubscriptionEndTimeTest(t, 12, sub)

	_, _, err := AdminUpdateUserSubscriptionEndTime(AdminUpdateUserSubscriptionEndTimeParams{
		UserSubscriptionId: sub.Id,
		UserId:             sub.UserId,
		ExpectedEndTime:    sub.EndTime - 1,
		EndTime:            now.Add(48 * time.Hour).Unix(),
		Action:             AdminUserSubscriptionEndTimeActionSet,
	})
	require.ErrorIs(t, err, ErrUserSubscriptionEndTimeChanged)

	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", sub.Id).Update("status", "cancelled").Error)
	_, _, err = AdminUpdateUserSubscriptionEndTime(AdminUpdateUserSubscriptionEndTimeParams{
		UserSubscriptionId: sub.Id,
		UserId:             sub.UserId,
		ExpectedEndTime:    sub.EndTime,
		EndTime:            now.Add(48 * time.Hour).Unix(),
		Action:             AdminUserSubscriptionEndTimeActionSet,
	})
	require.ErrorIs(t, err, ErrUserSubscriptionCancelled)
}

func TestAdminUpdateUserSubscriptionEndTimeReactivatesExpiredSubscription(t *testing.T) {
	truncateTables(t)
	now := time.Now()
	sub := &UserSubscription{
		Id:            103,
		PlanId:        7,
		StartTime:     now.AddDate(0, -2, 0).Unix(),
		EndTime:       now.AddDate(0, -1, 0).Unix(),
		Status:        "expired",
		Source:        "admin",
		UpgradeGroup:  "pro",
		PrevUserGroup: "default",
	}
	seedAdminSubscriptionEndTimeTest(t, 13, sub)

	newEndTime := now.AddDate(0, 1, 0).Unix()
	updated, message, err := AdminUpdateUserSubscriptionEndTime(AdminUpdateUserSubscriptionEndTimeParams{
		UserSubscriptionId: sub.Id,
		UserId:             sub.UserId,
		ExpectedEndTime:    sub.EndTime,
		EndTime:            newEndTime,
		Action:             AdminUserSubscriptionEndTimeActionSet,
	})
	require.NoError(t, err)
	assert.Equal(t, newEndTime, updated.EndTime)
	assert.Equal(t, "active", updated.Status)
	assert.Contains(t, message, "pro")

	var user User
	require.NoError(t, DB.Where("id = ?", sub.UserId).First(&user).Error)
	assert.Equal(t, "pro", user.Group)
}
