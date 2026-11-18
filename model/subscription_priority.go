package model

import (
	"errors"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SubscriptionConsumptionPreference is independent of quota snapshots, so an
// in-flight charge/reset cannot overwrite a newly saved consumption order.
type SubscriptionConsumptionPreference struct {
	UserId          int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	SubscriptionIds string `json:"-" gorm:"type:text;not null"`
	UpdatedAt       int64  `json:"updated_at" gorm:"type:bigint;not null"`
}

var ErrSubscriptionPriorityInvalid = errors.New("套餐顺序无效，请刷新后重试")

func getSubscriptionConsumptionPriorityTx(tx *gorm.DB, userId int) ([]int, error) {
	var preference SubscriptionConsumptionPreference
	query := tx.Where("user_id = ?", userId).Limit(1).Find(&preference)
	if query.Error == nil && query.RowsAffected == 0 {
		return []int{}, nil
	}
	if query.Error != nil {
		return nil, query.Error
	}
	ids := []int{}
	if err := common.UnmarshalJsonStr(preference.SubscriptionIds, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func GetSubscriptionConsumptionPriority(userId int) ([]int, error) {
	if userId <= 0 {
		return nil, ErrSubscriptionPriorityInvalid
	}
	return getSubscriptionConsumptionPriorityTx(DB, userId)
}

// Only the authenticated user's currently active subscriptions can be ranked.
// An empty list restores the historical earliest-expiry-first default. New or
// omitted subscriptions follow the explicit choices, in that default order.
func SetSubscriptionConsumptionPriority(userId int, ids []int) error {
	if userId <= 0 || len(ids) > 1000 {
		return ErrSubscriptionPriorityInvalid
	}
	seen := make(map[int]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return ErrSubscriptionPriorityInvalid
		}
		seen[id] = true
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		now := getDBTimestampTx(tx)
		if len(ids) > 0 {
			var count int64
			if err := tx.Model(&UserSubscription{}).
				Where("user_id = ? AND id IN ? AND status = ? AND end_time > ?", userId, ids, "active", now).
				Count(&count).Error; err != nil {
				return err
			}
			if count != int64(len(ids)) {
				return ErrSubscriptionPriorityInvalid
			}
		}
		if ids == nil {
			ids = []int{}
		}
		raw, err := common.Marshal(ids)
		if err != nil {
			return err
		}
		preference := SubscriptionConsumptionPreference{UserId: userId, SubscriptionIds: string(raw), UpdatedAt: now}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"subscription_ids", "updated_at"}),
		}).Create(&preference).Error
	})
}

func orderSubscriptionsForConsumption(subscriptions []UserSubscription, priority []int) {
	rank := make(map[int]int, len(priority))
	for i, id := range priority {
		rank[id] = i
	}
	sort.SliceStable(subscriptions, func(i, j int) bool {
		a, b := subscriptions[i], subscriptions[j]
		ra, aRanked := rank[a.Id]
		rb, bRanked := rank[b.Id]
		if aRanked != bRanked {
			return aRanked
		}
		if aRanked && ra != rb {
			return ra < rb
		}
		if a.EndTime != b.EndTime {
			return a.EndTime < b.EndTime
		}
		return a.Id < b.Id
	})
}
