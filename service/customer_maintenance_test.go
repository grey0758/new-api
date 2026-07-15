package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCustomerMaintenanceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:customer-maintenance-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.SubscriptionPlan{},
		&model.UserSubscription{},
		&model.CustomerContact{},
		&model.CustomerNotification{},
	))
	previousDB := model.DB
	previousSQLite := common.UsingSQLite
	previousMySQL := common.UsingMySQL
	previousPostgreSQL := common.UsingPostgreSQL
	model.DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.UsingSQLite = previousSQLite
		common.UsingMySQL = previousMySQL
		common.UsingPostgreSQL = previousPostgreSQL
	})
	return db
}

func seedCustomerMaintenanceUser(t *testing.T, db *gorm.DB, id int, role int, username string) model.User {
	t.Helper()
	user := model.User{
		Id:          id,
		Username:    username,
		Password:    "password123",
		DisplayName: strings.ToUpper(username),
		Role:        role,
		Status:      common.UserStatusEnabled,
		Email:       fmt.Sprintf("%s@example.com", username),
		Group:       "plus",
		AffCode:     fmt.Sprintf("aff-%d", id),
	}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func seedCustomerMaintenancePlan(t *testing.T, db *gorm.DB, id int, title string, unit string, value int) model.SubscriptionPlan {
	t.Helper()
	plan := model.SubscriptionPlan{
		Id:               id,
		Title:            title,
		PriceAmount:      999,
		Currency:         "USD",
		DurationUnit:     unit,
		DurationValue:    value,
		Enabled:          true,
		TotalAmount:      1000000,
		QuotaResetPeriod: model.SubscriptionResetNever,
	}
	require.NoError(t, db.Create(&plan).Error)
	return plan
}

func seedCustomerMaintenanceSubscription(t *testing.T, db *gorm.DB, id int, userId int, planId int, endTime int64, status string) {
	t.Helper()
	subscription := model.UserSubscription{
		Id:          id,
		UserId:      userId,
		PlanId:      planId,
		AmountTotal: 1000000,
		StartTime:   endTime - 30*24*60*60,
		EndTime:     endTime,
		Status:      status,
		Source:      "admin",
	}
	require.NoError(t, db.Create(&subscription).Error)
}

func TestBackfillExpiredMonthlySubscriptionNotifications(t *testing.T) {
	db := setupCustomerMaintenanceTestDB(t)
	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	monthlyPlan := seedCustomerMaintenancePlan(t, db, 1, "VIP月卡", model.SubscriptionDurationMonth, 1)
	dayPlan := seedCustomerMaintenancePlan(t, db, 2, "体验日卡", model.SubscriptionDurationDay, 1)
	otherMonthlyPlan := seedCustomerMaintenancePlan(t, db, 3, "其他额度月卡", model.SubscriptionDurationMonth, 1)
	require.NoError(t, db.Model(&otherMonthlyPlan).Update("total_amount", 9000000).Error)

	seedCustomerMaintenanceUser(t, db, 1, common.RoleCommonUser, "expired")
	seedCustomerMaintenanceSubscription(t, db, 1, 1, monthlyPlan.Id, now.AddDate(0, -1, 0).Unix(), "expired")

	seedCustomerMaintenanceUser(t, db, 2, common.RoleCommonUser, "renewed")
	seedCustomerMaintenanceSubscription(t, db, 2, 2, monthlyPlan.Id, now.AddDate(0, -1, 0).Unix(), "expired")
	seedCustomerMaintenanceSubscription(t, db, 3, 2, otherMonthlyPlan.Id, now.AddDate(0, 1, 0).Unix(), "active")

	seedCustomerMaintenanceUser(t, db, 3, common.RoleCommonUser, "daycard")
	seedCustomerMaintenanceSubscription(t, db, 4, 3, dayPlan.Id, now.AddDate(0, -1, 0).Unix(), "expired")

	seedCustomerMaintenanceUser(t, db, 4, common.RoleAdminUser, "admin")
	seedCustomerMaintenanceSubscription(t, db, 5, 4, monthlyPlan.Id, now.AddDate(0, -1, 0).Unix(), "expired")

	seedCustomerMaintenanceUser(t, db, 5, common.RoleCommonUser, "cancelled")
	seedCustomerMaintenanceSubscription(t, db, 6, 5, monthlyPlan.Id, now.AddDate(0, -1, 0).Unix(), "cancelled")

	seedCustomerMaintenanceUser(t, db, 6, common.RoleCommonUser, "old")
	seedCustomerMaintenanceSubscription(t, db, 7, 6, monthlyPlan.Id, now.AddDate(0, -3, 0).Unix(), "expired")

	seedCustomerMaintenanceUser(t, db, 7, common.RoleCommonUser, "latest")
	seedCustomerMaintenanceSubscription(t, db, 8, 7, monthlyPlan.Id, now.AddDate(0, -2, 5).Unix(), "expired")
	seedCustomerMaintenanceSubscription(t, db, 9, 7, monthlyPlan.Id, now.AddDate(0, -1, 5).Unix(), "expired")

	seedCustomerMaintenanceUser(t, db, 8, common.RoleCommonUser, "day-renewal")
	seedCustomerMaintenanceSubscription(t, db, 10, 8, monthlyPlan.Id, now.AddDate(0, -1, 0).Unix(), "expired")
	seedCustomerMaintenanceSubscription(t, db, 11, 8, dayPlan.Id, now.AddDate(0, 1, 0).Unix(), "active")

	seedCustomerMaintenanceUser(t, db, 9, common.RoleCommonUser, "same-plan-renewal")
	seedCustomerMaintenanceSubscription(t, db, 12, 9, monthlyPlan.Id, now.AddDate(0, -1, 0).Unix(), "expired")
	seedCustomerMaintenanceSubscription(t, db, 13, 9, monthlyPlan.Id, now.AddDate(0, 1, 0).Unix(), "active")

	result, err := BackfillExpiredMonthlySubscriptionNotifications(2, now)
	require.NoError(t, err)
	assert.Equal(t, 3, result.MatchedExpiredUsers)
	assert.Equal(t, int64(3), result.Created)
	assert.Equal(t, int64(0), result.Existing)
	assert.Equal(t, 2, result.SkippedCurrent)
	assert.Equal(t, int64(0), result.SupersededRenewals)

	var notifications []model.CustomerNotification
	require.NoError(t, db.Order("user_id ASC").Find(&notifications).Error)
	require.Len(t, notifications, 3)
	assert.Equal(t, []int{1, 7, 8}, []int{notifications[0].UserId, notifications[1].UserId, notifications[2].UserId})
	assert.Equal(t, 9, notifications[1].SubscriptionId)
	assert.Equal(t, model.CustomerNotificationPushStatusUnconfigured, notifications[0].PushStatus)
	assert.Contains(t, notifications[0].Content, "VIP月卡")
	assert.Contains(t, notifications[0].Content, "微信联系")

	second, err := BackfillExpiredMonthlySubscriptionNotifications(2, now)
	require.NoError(t, err)
	assert.Equal(t, int64(0), second.Created)
	assert.Equal(t, int64(3), second.Existing)
	var count int64
	require.NoError(t, db.Model(&model.CustomerNotification{}).Count(&count).Error)
	assert.Equal(t, int64(3), count)
}

func TestBackfillSupersedesPendingNotificationAfterDifferentMonthlyPlanRenewal(t *testing.T) {
	db := setupCustomerMaintenanceTestDB(t)
	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	expiredPlan := seedCustomerMaintenancePlan(t, db, 21, "基础月卡", model.SubscriptionDurationMonth, 1)
	renewalPlan := seedCustomerMaintenancePlan(t, db, 22, "高额度月卡", model.SubscriptionDurationMonth, 1)
	require.NoError(t, db.Model(&renewalPlan).Update("total_amount", 12000000).Error)
	user := seedCustomerMaintenanceUser(t, db, 21, common.RoleCommonUser, "renewed-later")
	expiredEnd := now.AddDate(0, -1, 0).Unix()
	seedCustomerMaintenanceSubscription(t, db, 21, user.Id, expiredPlan.Id, expiredEnd, "expired")

	first, err := BackfillExpiredMonthlySubscriptionNotifications(2, now)
	require.NoError(t, err)
	assert.Equal(t, int64(1), first.Created)
	assert.Equal(t, int64(0), first.SupersededRenewals)

	renewalEnd := now.AddDate(0, 1, 0).Unix()
	seedCustomerMaintenanceSubscription(t, db, 22, user.Id, renewalPlan.Id, renewalEnd, "active")
	second, err := BackfillExpiredMonthlySubscriptionNotifications(2, now)
	require.NoError(t, err)
	assert.Equal(t, int64(0), second.Created)
	assert.Equal(t, int64(1), second.SupersededRenewals)

	var notification model.CustomerNotification
	require.NoError(t, db.First(&notification).Error)
	assert.Equal(t, model.CustomerNotificationStatusSuperseded, notification.Status)

	visible, visibleTotal, err := model.ListCustomerMaintenanceNotifications("", "", "", 0, 20)
	require.NoError(t, err)
	assert.Empty(t, visible)
	assert.Equal(t, int64(0), visibleTotal)

	history, historyTotal, err := model.ListCustomerMaintenanceNotifications("", model.CustomerNotificationStatusSuperseded, "", 0, 20)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, int64(1), historyTotal)
	assert.Equal(t, expiredEnd-30*24*60*60, history[0].SubscriptionStartTime)

	customers, customerTotal, err := model.ListCustomerMaintenanceCustomers("renewed-later", 0, 20)
	require.NoError(t, err)
	require.Len(t, customers, 1)
	assert.Equal(t, int64(1), customerTotal)
	assert.Equal(t, renewalEnd-30*24*60*60, customers[0].LatestSubscriptionStartTime)

	third, err := BackfillExpiredMonthlySubscriptionNotifications(2, now)
	require.NoError(t, err)
	assert.Equal(t, int64(0), third.SupersededRenewals)
}

func TestCustomerContactUsesSidecarTable(t *testing.T) {
	db := setupCustomerMaintenanceTestDB(t)
	user := seedCustomerMaintenanceUser(t, db, 11, common.RoleCommonUser, "contact")
	contact, err := model.UpsertCustomerContact(user.Id, "wx-contact", "客户昵称", "月卡客户", true)
	require.NoError(t, err)
	assert.Equal(t, "wx-contact", contact.WeChatAccount)
	assert.Equal(t, "客户昵称", contact.WeChatUsername)

	var storedUser model.User
	require.NoError(t, db.First(&storedUser, user.Id).Error)
	assert.Empty(t, storedUser.WeChatId, "customer maintenance must not write the upstream users.wechat_id column")

	items, total, err := model.ListCustomerMaintenanceCustomers("客户昵称", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, user.Id, items[0].UserId)
	assert.Equal(t, "wx-contact", items[0].WeChatAccount)
	assert.Equal(t, "客户昵称", items[0].WeChatUsername)
}

func TestCustomerMaintenanceModuleEnabled(t *testing.T) {
	testCases := []struct {
		name      string
		rawConfig string
		expected  bool
	}{
		{name: "missing", rawConfig: "", expected: false},
		{name: "invalid", rawConfig: "not-json", expected: false},
		{name: "boolean enabled", rawConfig: `{"customerMaintenance":true}`, expected: true},
		{name: "boolean disabled", rawConfig: `{"customerMaintenance":false}`, expected: false},
		{name: "object enabled", rawConfig: `{"customerMaintenance":{"enabled":true}}`, expected: true},
		{name: "object disabled", rawConfig: `{"customerMaintenance":{"enabled":false}}`, expected: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, customerMaintenanceModuleEnabled(testCase.rawConfig))
		})
	}
}
