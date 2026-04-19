package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm/clause"
)

type RelayResponseRef struct {
	ID          int    `json:"id"`
	ResponseID  string `json:"response_id" gorm:"uniqueIndex;size:191;not null"`
	UserID      int    `json:"user_id" gorm:"index;not null"`
	TokenID     int    `json:"token_id" gorm:"index"`
	ChannelID   int    `json:"channel_id" gorm:"index;not null"`
	ModelName   string `json:"model_name" gorm:"size:255"`
	CreatedTime int64  `json:"created_time" gorm:"bigint;index"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint"`
}

func UpsertRelayResponseRef(ref *RelayResponseRef) error {
	if ref == nil {
		return nil
	}
	ref.ResponseID = strings.TrimSpace(ref.ResponseID)
	if ref.ResponseID == "" || ref.ChannelID == 0 {
		return nil
	}
	now := common.GetTimestamp()
	if ref.CreatedTime == 0 {
		ref.CreatedTime = now
	}
	ref.UpdatedTime = now
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "response_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"user_id":      ref.UserID,
			"token_id":     ref.TokenID,
			"channel_id":   ref.ChannelID,
			"model_name":   ref.ModelName,
			"updated_time": ref.UpdatedTime,
		}),
	}).Create(ref).Error
}

func GetRelayResponseRefByResponseID(responseID string) (*RelayResponseRef, error) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return nil, nil
	}
	var ref RelayResponseRef
	err := DB.Where("response_id = ?", responseID).First(&ref).Error
	if err != nil {
		return nil, err
	}
	return &ref, nil
}
