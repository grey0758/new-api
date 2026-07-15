package model

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	CustomerNotificationEventSubscriptionExpired = "subscription_expired"
	CustomerNotificationStatusPending            = "pending"
	CustomerNotificationStatusAcknowledged       = "acknowledged"
	CustomerNotificationStatusSuperseded         = "superseded"
	CustomerNotificationPushChannelWeChatBot     = "wechat_bot"
	CustomerNotificationPushStatusUnconfigured   = "unconfigured"
)

var ErrCustomerMaintenanceUserNotFound = errors.New("customer maintenance user not found")

// CustomerContact keeps OpenCodex-specific customer contact data outside the
// upstream users table. UserId is a logical foreign key validated by the
// application so migrations stay portable across SQLite, MySQL, and PostgreSQL.
type CustomerContact struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	UserId         int    `json:"user_id" gorm:"not null;uniqueIndex"`
	WeChatAccount  string `json:"wechat_account" gorm:"column:wechat_account;type:varchar(128);index"`
	WeChatUsername string `json:"wechat_username" gorm:"column:wechat_username;type:varchar(128);index"`
	PushEnabled    bool   `json:"push_enabled"`
	Notes          string `json:"notes" gorm:"type:varchar(512)"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint"`
}

func (CustomerContact) TableName() string {
	return "opencodex_customer_contacts"
}

func (contact *CustomerContact) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	contact.CreatedAt = now
	contact.UpdatedAt = now
	return nil
}

func (contact *CustomerContact) BeforeUpdate(tx *gorm.DB) error {
	contact.UpdatedAt = common.GetTimestamp()
	return nil
}

// CustomerNotification is both the durable subscription event and the
// reserved outbound-message slot. A future robot adapter can claim rows by
// PushStatus without changing the subscription or users tables.
type CustomerNotification struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	EventKey       string `json:"event_key" gorm:"type:varchar(191);not null;uniqueIndex"`
	EventType      string `json:"event_type" gorm:"type:varchar(64);not null;index"`
	UserId         int    `json:"user_id" gorm:"not null;index;index:idx_oc_customer_notification_user_status,priority:1"`
	SubscriptionId int    `json:"subscription_id" gorm:"not null;index"`
	PlanId         int    `json:"plan_id" gorm:"not null;index"`
	Title          string `json:"title" gorm:"type:varchar(255);not null"`
	Content        string `json:"content" gorm:"type:text;not null"`
	OccurredAt     int64  `json:"occurred_at" gorm:"bigint;not null;index"`
	Status         string `json:"status" gorm:"type:varchar(32);not null;index;index:idx_oc_customer_notification_user_status,priority:2"`
	AcknowledgedAt int64  `json:"acknowledged_at" gorm:"bigint"`
	PushChannel    string `json:"push_channel" gorm:"type:varchar(32);not null;index"`
	PushTarget     string `json:"push_target" gorm:"type:varchar(255)"`
	PushStatus     string `json:"push_status" gorm:"type:varchar(32);not null;index"`
	PushAttempts   int    `json:"push_attempts"`
	LastPushError  string `json:"last_push_error" gorm:"type:text"`
	PushedAt       int64  `json:"pushed_at" gorm:"bigint"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint"`
}

func (CustomerNotification) TableName() string {
	return "opencodex_customer_notifications"
}

func (notification *CustomerNotification) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if notification.Status == "" {
		notification.Status = CustomerNotificationStatusPending
	}
	if notification.PushChannel == "" {
		notification.PushChannel = CustomerNotificationPushChannelWeChatBot
	}
	if notification.PushStatus == "" {
		notification.PushStatus = CustomerNotificationPushStatusUnconfigured
	}
	notification.CreatedAt = now
	notification.UpdatedAt = now
	return nil
}

func (notification *CustomerNotification) BeforeUpdate(tx *gorm.DB) error {
	notification.UpdatedAt = common.GetTimestamp()
	return nil
}

type CustomerMaintenanceCustomer struct {
	UserId                      int    `json:"user_id"`
	Username                    string `json:"username"`
	DisplayName                 string `json:"display_name"`
	Email                       string `json:"email"`
	Group                       string `json:"group"`
	UserStatus                  int    `json:"user_status"`
	WeChatAccount               string `json:"wechat_account"`
	WeChatUsername              string `json:"wechat_username"`
	PushEnabled                 bool   `json:"push_enabled"`
	Notes                       string `json:"notes"`
	LatestSubscriptionId        int    `json:"latest_subscription_id"`
	LatestPlanId                int    `json:"latest_plan_id"`
	LatestPlanTitle             string `json:"latest_plan_title"`
	LatestSubscriptionStatus    string `json:"latest_subscription_status"`
	LatestSubscriptionStartTime int64  `json:"latest_subscription_start_time"`
	LatestSubscriptionEndTime   int64  `json:"latest_subscription_end_time"`
}

type CustomerMaintenanceNotification struct {
	CustomerNotification
	Username              string `json:"username"`
	DisplayName           string `json:"display_name"`
	Email                 string `json:"email"`
	WeChatAccount         string `json:"wechat_account"`
	WeChatUsername        string `json:"wechat_username"`
	ContactPushEnabled    bool   `json:"contact_push_enabled"`
	PlanTitle             string `json:"plan_title"`
	SubscriptionStatus    string `json:"subscription_status"`
	SubscriptionStartTime int64  `json:"subscription_start_time"`
	SubscriptionEndTime   int64  `json:"subscription_end_time"`
}

type ExpiredMonthlySubscriptionCandidate struct {
	SubscriptionId int    `json:"subscription_id"`
	UserId         int    `json:"user_id"`
	PlanId         int    `json:"plan_id"`
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
	PlanTitle      string `json:"plan_title"`
	EndTime        int64  `json:"end_time"`
	Status         string `json:"status"`
}

func findCustomerMaintenanceUserIds(keyword string) ([]int, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	pattern := "%" + keyword + "%"
	userQuery := DB.Model(&User{}).
		Where("deleted_at IS NULL").
		Where("role < ?", common.RoleAdminUser)
	if id, err := strconv.Atoi(keyword); err == nil && id > 0 {
		userQuery = userQuery.Where(
			"id = ? OR username LIKE ? OR display_name LIKE ? OR email LIKE ?",
			id,
			pattern,
			pattern,
			pattern,
		)
	} else {
		userQuery = userQuery.Where(
			"username LIKE ? OR display_name LIKE ? OR email LIKE ?",
			pattern,
			pattern,
			pattern,
		)
	}
	var userIds []int
	if err := userQuery.Pluck("id", &userIds).Error; err != nil {
		return nil, err
	}
	var contactUserIds []int
	if err := DB.Model(&CustomerContact{}).
		Where("wechat_account LIKE ? OR wechat_username LIKE ?", pattern, pattern).
		Pluck("user_id", &contactUserIds).Error; err != nil {
		return nil, err
	}
	seen := make(map[int]struct{}, len(userIds)+len(contactUserIds))
	result := make([]int, 0, len(userIds)+len(contactUserIds))
	for _, userId := range append(userIds, contactUserIds...) {
		if userId <= 0 {
			continue
		}
		if _, ok := seen[userId]; ok {
			continue
		}
		seen[userId] = struct{}{}
		result = append(result, userId)
	}
	return result, nil
}

func ListCustomerMaintenanceCustomers(keyword string, startIdx int, pageSize int) ([]CustomerMaintenanceCustomer, int64, error) {
	query := DB.Model(&User{}).
		Where("deleted_at IS NULL").
		Where("role < ?", common.RoleAdminUser)
	if strings.TrimSpace(keyword) != "" {
		userIds, err := findCustomerMaintenanceUserIds(keyword)
		if err != nil {
			return nil, 0, err
		}
		if len(userIds) == 0 {
			return []CustomerMaintenanceCustomer{}, 0, nil
		}
		query = query.Where("id IN ?", userIds)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []User
	if err := query.Session(&gorm.Session{}).
		Order("id DESC").
		Limit(pageSize).
		Offset(startIdx).
		Omit("password").
		Find(&users).Error; err != nil {
		return nil, 0, err
	}
	if len(users) == 0 {
		return []CustomerMaintenanceCustomer{}, total, nil
	}
	userIds := make([]int, 0, len(users))
	for _, user := range users {
		userIds = append(userIds, user.Id)
	}
	contacts, err := getCustomerContactsByUserIds(userIds)
	if err != nil {
		return nil, 0, err
	}
	latestSubscriptions, plans, err := getLatestSubscriptionsAndPlans(userIds)
	if err != nil {
		return nil, 0, err
	}
	items := make([]CustomerMaintenanceCustomer, 0, len(users))
	for _, user := range users {
		item := CustomerMaintenanceCustomer{
			UserId:      user.Id,
			Username:    user.Username,
			DisplayName: user.DisplayName,
			Email:       user.Email,
			Group:       user.Group,
			UserStatus:  user.Status,
		}
		if contact, ok := contacts[user.Id]; ok {
			item.WeChatAccount = contact.WeChatAccount
			item.WeChatUsername = contact.WeChatUsername
			item.PushEnabled = contact.PushEnabled
			item.Notes = contact.Notes
		}
		if subscription, ok := latestSubscriptions[user.Id]; ok {
			item.LatestSubscriptionId = subscription.Id
			item.LatestPlanId = subscription.PlanId
			item.LatestSubscriptionStatus = subscription.Status
			item.LatestSubscriptionStartTime = subscription.StartTime
			item.LatestSubscriptionEndTime = subscription.EndTime
			if plan, ok := plans[subscription.PlanId]; ok {
				item.LatestPlanTitle = plan.Title
			}
		}
		items = append(items, item)
	}
	return items, total, nil
}

func ListCustomerMaintenanceNotifications(keyword string, status string, pushStatus string, startIdx int, pageSize int) ([]CustomerMaintenanceNotification, int64, error) {
	query := DB.Model(&CustomerNotification{})
	if status = strings.TrimSpace(status); status != "" {
		query = query.Where("status = ?", status)
	} else {
		query = query.Where("status <> ?", CustomerNotificationStatusSuperseded)
	}
	if pushStatus = strings.TrimSpace(pushStatus); pushStatus != "" {
		query = query.Where("push_status = ?", pushStatus)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		userIds, err := findCustomerMaintenanceUserIds(keyword)
		if err != nil {
			return nil, 0, err
		}
		if notificationId, err := strconv.Atoi(keyword); err == nil && notificationId > 0 {
			if len(userIds) > 0 {
				query = query.Where("id = ? OR user_id IN ?", notificationId, userIds)
			} else {
				query = query.Where("id = ?", notificationId)
			}
		} else if len(userIds) > 0 {
			query = query.Where("user_id IN ?", userIds)
		} else {
			return []CustomerMaintenanceNotification{}, 0, nil
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var notifications []CustomerNotification
	if err := query.Session(&gorm.Session{}).
		Order("occurred_at DESC, id DESC").
		Limit(pageSize).
		Offset(startIdx).
		Find(&notifications).Error; err != nil {
		return nil, 0, err
	}
	if len(notifications) == 0 {
		return []CustomerMaintenanceNotification{}, total, nil
	}
	userIds := make([]int, 0, len(notifications))
	subscriptionIds := make([]int, 0, len(notifications))
	planIds := make([]int, 0, len(notifications))
	for _, notification := range notifications {
		userIds = append(userIds, notification.UserId)
		subscriptionIds = append(subscriptionIds, notification.SubscriptionId)
		planIds = append(planIds, notification.PlanId)
	}
	var users []User
	if err := DB.Unscoped().Where("id IN ?", userIds).Omit("password").Find(&users).Error; err != nil {
		return nil, 0, err
	}
	userMap := make(map[int]User, len(users))
	for _, user := range users {
		userMap[user.Id] = user
	}
	contacts, err := getCustomerContactsByUserIds(userIds)
	if err != nil {
		return nil, 0, err
	}
	var subscriptions []UserSubscription
	if err := DB.Where("id IN ?", subscriptionIds).Find(&subscriptions).Error; err != nil {
		return nil, 0, err
	}
	subscriptionMap := make(map[int]UserSubscription, len(subscriptions))
	for _, subscription := range subscriptions {
		subscriptionMap[subscription.Id] = subscription
	}
	var planList []SubscriptionPlan
	if err := DB.Where("id IN ?", planIds).Find(&planList).Error; err != nil {
		return nil, 0, err
	}
	planMap := make(map[int]SubscriptionPlan, len(planList))
	for _, plan := range planList {
		planMap[plan.Id] = plan
	}
	items := make([]CustomerMaintenanceNotification, 0, len(notifications))
	for _, notification := range notifications {
		item := CustomerMaintenanceNotification{CustomerNotification: notification}
		if user, ok := userMap[notification.UserId]; ok {
			item.Username = user.Username
			item.DisplayName = user.DisplayName
			item.Email = user.Email
		}
		if contact, ok := contacts[notification.UserId]; ok {
			item.WeChatAccount = contact.WeChatAccount
			item.WeChatUsername = contact.WeChatUsername
			item.ContactPushEnabled = contact.PushEnabled
		}
		if plan, ok := planMap[notification.PlanId]; ok {
			item.PlanTitle = plan.Title
		}
		if subscription, ok := subscriptionMap[notification.SubscriptionId]; ok {
			item.SubscriptionStatus = subscription.Status
			item.SubscriptionStartTime = subscription.StartTime
			item.SubscriptionEndTime = subscription.EndTime
		}
		items = append(items, item)
	}
	return items, total, nil
}

func getCustomerContactsByUserIds(userIds []int) (map[int]CustomerContact, error) {
	result := make(map[int]CustomerContact)
	if len(userIds) == 0 {
		return result, nil
	}
	var contacts []CustomerContact
	if err := DB.Where("user_id IN ?", userIds).Find(&contacts).Error; err != nil {
		return nil, err
	}
	for _, contact := range contacts {
		result[contact.UserId] = contact
	}
	return result, nil
}

func getLatestSubscriptionsAndPlans(userIds []int) (map[int]UserSubscription, map[int]SubscriptionPlan, error) {
	latest := make(map[int]UserSubscription)
	plans := make(map[int]SubscriptionPlan)
	if len(userIds) == 0 {
		return latest, plans, nil
	}
	var subscriptions []UserSubscription
	if err := DB.Where("user_id IN ?", userIds).
		Order("user_id ASC, end_time DESC, id DESC").
		Find(&subscriptions).Error; err != nil {
		return nil, nil, err
	}
	planIds := make([]int, 0)
	for _, subscription := range subscriptions {
		if _, ok := latest[subscription.UserId]; ok {
			continue
		}
		latest[subscription.UserId] = subscription
		planIds = append(planIds, subscription.PlanId)
	}
	if len(planIds) == 0 {
		return latest, plans, nil
	}
	var planList []SubscriptionPlan
	if err := DB.Where("id IN ?", planIds).Find(&planList).Error; err != nil {
		return nil, nil, err
	}
	for _, plan := range planList {
		plans[plan.Id] = plan
	}
	return latest, plans, nil
}

func UpsertCustomerContact(userId int, weChatAccount string, weChatUsername string, notes string, pushEnabled bool) (*CustomerContact, error) {
	weChatAccount = strings.TrimSpace(weChatAccount)
	weChatUsername = strings.TrimSpace(weChatUsername)
	notes = strings.TrimSpace(notes)
	var contact CustomerContact
	err := DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&User{}).
			Where("id = ? AND deleted_at IS NULL AND role < ?", userId, common.RoleAdminUser).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrCustomerMaintenanceUserNotFound
		}
		err := tx.Where("user_id = ?", userId).First(&contact).Error
		now := common.GetTimestamp()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			contact = CustomerContact{
				UserId:         userId,
				WeChatAccount:  weChatAccount,
				WeChatUsername: weChatUsername,
				PushEnabled:    pushEnabled,
				Notes:          notes,
			}
			return tx.Create(&contact).Error
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&contact).Updates(map[string]interface{}{
			"wechat_account":  weChatAccount,
			"wechat_username": weChatUsername,
			"push_enabled":    pushEnabled,
			"notes":           notes,
			"updated_at":      now,
		}).Error; err != nil {
			return err
		}
		contact.WeChatAccount = weChatAccount
		contact.WeChatUsername = weChatUsername
		contact.PushEnabled = pushEnabled
		contact.Notes = notes
		contact.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &contact, nil
}

func FindMonthlySubscriptionCandidates(windowStart int64) ([]ExpiredMonthlySubscriptionCandidate, error) {
	var candidates []ExpiredMonthlySubscriptionCandidate
	err := DB.Table("user_subscriptions").
		Select(
			"user_subscriptions.id AS subscription_id, "+
				"user_subscriptions.user_id AS user_id, "+
				"user_subscriptions.plan_id AS plan_id, "+
				"users.username AS username, "+
				"users.display_name AS display_name, "+
				"subscription_plans.title AS plan_title, "+
				"user_subscriptions.end_time AS end_time, "+
				"user_subscriptions.status AS status",
		).
		Joins("JOIN subscription_plans ON subscription_plans.id = user_subscriptions.plan_id").
		Joins("JOIN users ON users.id = user_subscriptions.user_id").
		Where("subscription_plans.duration_unit = ?", SubscriptionDurationMonth).
		Where("subscription_plans.duration_value = ?", 1).
		Where("user_subscriptions.end_time >= ?", windowStart).
		Where("user_subscriptions.status IN ?", []string{"active", "expired"}).
		Where("users.deleted_at IS NULL").
		Where("users.role < ?", common.RoleAdminUser).
		Order("user_subscriptions.user_id ASC, user_subscriptions.end_time DESC, user_subscriptions.id DESC").
		Scan(&candidates).Error
	return candidates, err
}

func InsertCustomerNotificationsIgnoreConflicts(notifications []CustomerNotification) (int64, error) {
	if len(notifications) == 0 {
		return 0, nil
	}
	result := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "event_key"}},
		DoNothing: true,
	}).CreateInBatches(&notifications, 100)
	return result.RowsAffected, result.Error
}

func SupersedePendingCustomerNotificationsForRenewedMonthlySubscribers(now int64) (int64, error) {
	var renewedUserIds []int
	err := DB.Table("user_subscriptions").
		Distinct("user_subscriptions.user_id").
		Joins("JOIN subscription_plans ON subscription_plans.id = user_subscriptions.plan_id").
		Joins("JOIN opencodex_customer_notifications ON opencodex_customer_notifications.user_id = user_subscriptions.user_id").
		Where("subscription_plans.duration_unit = ?", SubscriptionDurationMonth).
		Where("subscription_plans.duration_value = ?", 1).
		Where("user_subscriptions.status IN ?", []string{"active", "expired"}).
		Where("user_subscriptions.end_time > ?", now).
		Where("opencodex_customer_notifications.event_type = ?", CustomerNotificationEventSubscriptionExpired).
		Where("opencodex_customer_notifications.status = ?", CustomerNotificationStatusPending).
		Pluck("user_subscriptions.user_id", &renewedUserIds).Error
	if err != nil || len(renewedUserIds) == 0 {
		return 0, err
	}
	result := DB.Model(&CustomerNotification{}).
		Where("user_id IN ?", renewedUserIds).
		Where("event_type = ?", CustomerNotificationEventSubscriptionExpired).
		Where("status = ?", CustomerNotificationStatusPending).
		Updates(map[string]interface{}{
			"status":     CustomerNotificationStatusSuperseded,
			"updated_at": common.GetTimestamp(),
		})
	return result.RowsAffected, result.Error
}

func AcknowledgeCustomerNotification(id int) error {
	if id <= 0 {
		return errors.New("invalid customer notification id")
	}
	now := common.GetTimestamp()
	result := DB.Model(&CustomerNotification{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":          CustomerNotificationStatusAcknowledged,
			"acknowledged_at": now,
			"updated_at":      now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
