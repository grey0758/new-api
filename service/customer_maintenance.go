package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
)

type CustomerMaintenanceBackfillResult struct {
	WindowStart         int64 `json:"window_start"`
	WindowEnd           int64 `json:"window_end"`
	CandidateRows       int   `json:"candidate_rows"`
	MatchedExpiredUsers int   `json:"matched_expired_users"`
	Created             int64 `json:"created"`
	Existing            int64 `json:"existing"`
	SkippedCurrent      int   `json:"skipped_current"`
	SupersededRenewals  int64 `json:"superseded_renewals"`
}

func customerMaintenanceLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return location
}

func BackfillExpiredMonthlySubscriptionNotifications(months int, now time.Time) (*CustomerMaintenanceBackfillResult, error) {
	if months <= 0 {
		months = 2
	}
	if months > 12 {
		months = 12
	}
	location := customerMaintenanceLocation()
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(location)
	windowStart := now.AddDate(0, -months, 0).Unix()
	windowEnd := now.Unix()
	superseded, err := model.SupersedePendingCustomerNotificationsForRenewedMonthlySubscribers(windowEnd)
	if err != nil {
		return nil, err
	}
	candidates, err := model.FindMonthlySubscriptionCandidates(windowStart)
	if err != nil {
		return nil, err
	}
	result := &CustomerMaintenanceBackfillResult{
		WindowStart:        windowStart,
		WindowEnd:          windowEnd,
		CandidateRows:      len(candidates),
		SupersededRenewals: superseded,
	}
	seenUsers := make(map[int]struct{})
	notifications := make([]model.CustomerNotification, 0)
	for _, candidate := range candidates {
		if _, ok := seenUsers[candidate.UserId]; ok {
			continue
		}
		seenUsers[candidate.UserId] = struct{}{}
		if candidate.EndTime > windowEnd {
			result.SkippedCurrent++
			continue
		}
		if candidate.EndTime < windowStart {
			continue
		}
		result.MatchedExpiredUsers++
		customerName := strings.TrimSpace(candidate.DisplayName)
		if customerName == "" {
			customerName = strings.TrimSpace(candidate.Username)
		}
		if customerName == "" {
			customerName = fmt.Sprintf("用户 %d", candidate.UserId)
		}
		planTitle := strings.TrimSpace(candidate.PlanTitle)
		if planTitle == "" {
			planTitle = "月卡"
		}
		expiredAt := time.Unix(candidate.EndTime, 0).In(location).Format("2006-01-02 15:04:05")
		notifications = append(notifications, model.CustomerNotification{
			EventKey:       fmt.Sprintf("subscription_expired:%d:%d", candidate.SubscriptionId, candidate.EndTime),
			EventType:      model.CustomerNotificationEventSubscriptionExpired,
			UserId:         candidate.UserId,
			SubscriptionId: candidate.SubscriptionId,
			PlanId:         candidate.PlanId,
			Title:          "月卡到期提醒",
			Content: fmt.Sprintf(
				"用户 %s（ID %d）的月卡「%s」已于 %s 到期，请跟进续费与微信联系。",
				customerName,
				candidate.UserId,
				planTitle,
				expiredAt,
			),
			OccurredAt:  candidate.EndTime,
			Status:      model.CustomerNotificationStatusPending,
			PushChannel: model.CustomerNotificationPushChannelWeChatBot,
			PushStatus:  model.CustomerNotificationPushStatusUnconfigured,
		})
	}
	created, err := model.InsertCustomerNotificationsIgnoreConflicts(notifications)
	if err != nil {
		return nil, err
	}
	result.Created = created
	result.Existing = int64(len(notifications)) - created
	return result, nil
}
