package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	GeneratedImageRequestStatusProcessing    = "processing"
	GeneratedImageRequestStatusStored        = "stored"
	GeneratedImageRequestStatusSucceeded     = "succeeded"
	GeneratedImageRequestStatusFailed        = "failed"
	GeneratedImageRequestStatusStorageFailed = "storage_failed"
)

type GeneratedImageRequest struct {
	Id                   int64  `json:"id"`
	UserId               int    `json:"user_id" gorm:"uniqueIndex:idx_generated_image_user_idempotency,priority:1;index"`
	TokenId              int    `json:"token_id" gorm:"index"`
	RequestId            string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	IdempotencyKeyDigest string `json:"-" gorm:"type:char(64);uniqueIndex:idx_generated_image_user_idempotency,priority:2"`
	RequestHash          string `json:"-" gorm:"type:char(64);not null"`
	Model                string `json:"model" gorm:"type:varchar(191);index"`
	ResponseFormat       string `json:"response_format" gorm:"type:varchar(32);default:'url'"`
	Status               string `json:"status" gorm:"type:varchar(32);index"`
	ErrorCode            string `json:"error_code,omitempty" gorm:"type:varchar(64);default:''"`
	ErrorStatus          int    `json:"error_status,omitempty" gorm:"type:int;default:0"`
	ExpiresAt            int64  `json:"expires_at" gorm:"bigint;index"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt            int64  `json:"updated_at" gorm:"bigint;index"`
}

func (GeneratedImageRequest) TableName() string {
	return "opencodex_generated_image_requests"
}

func (r *GeneratedImageRequest) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	if r.Status == "" {
		r.Status = GeneratedImageRequestStatusProcessing
	}
	if r.ResponseFormat == "" {
		r.ResponseFormat = "url"
	}
	return nil
}

func (r *GeneratedImageRequest) BeforeUpdate(tx *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

type GeneratedAsset struct {
	Id          int64  `json:"id"`
	TaskId      string `json:"task_id" gorm:"type:varchar(64);uniqueIndex"`
	RequestId   string `json:"request_id" gorm:"type:varchar(64);index"`
	UserId      int    `json:"user_id" gorm:"index"`
	TokenId     int    `json:"token_id" gorm:"index"`
	Model       string `json:"model" gorm:"type:varchar(191);index"`
	ChannelId   int    `json:"channel_id" gorm:"index"`
	ObjectKey   string `json:"-" gorm:"type:varchar(512);not null"`
	ContentType string `json:"content_type" gorm:"type:varchar(128);not null"`
	SizeBytes   int64  `json:"size_bytes" gorm:"bigint;not null"`
	Width       int    `json:"width" gorm:"type:int;not null;default:0"`
	Height      int    `json:"height" gorm:"type:int;not null;default:0"`
	SHA256      string `json:"sha256" gorm:"type:char(64);not null;index"`
	ExpiresAt   int64  `json:"expires_at" gorm:"bigint;index"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint;index"`
}

func (GeneratedAsset) TableName() string {
	return "opencodex_generated_assets"
}

func (a *GeneratedAsset) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	a.CreatedAt = now
	a.UpdatedAt = now
	return nil
}

func (a *GeneratedAsset) BeforeUpdate(tx *gorm.DB) error {
	a.UpdatedAt = common.GetTimestamp()
	return nil
}

func BeginGeneratedImageRequest(request *GeneratedImageRequest) (*GeneratedImageRequest, bool, error) {
	if request == nil {
		return nil, false, errors.New("generated image request is nil")
	}
	if request.UserId <= 0 || request.RequestId == "" || request.IdempotencyKeyDigest == "" || request.RequestHash == "" {
		return nil, false, errors.New("generated image request identity is incomplete")
	}

	result := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(request)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		return request, true, nil
	}

	var existing GeneratedImageRequest
	if err := DB.Where("user_id = ? AND idempotency_key_digest = ?", request.UserId, request.IdempotencyKeyDigest).
		First(&existing).Error; err != nil {
		return nil, false, err
	}
	return &existing, false, nil
}

func GetGeneratedImageRequestByRequestId(requestId string) (*GeneratedImageRequest, error) {
	if requestId == "" {
		return nil, errors.New("requestId is empty")
	}
	var request GeneratedImageRequest
	if err := DB.Where("request_id = ?", requestId).First(&request).Error; err != nil {
		return nil, err
	}
	return &request, nil
}

func MarkGeneratedImageRequestFailed(requestId string, status string, errorStatus int, errorCode string) error {
	if requestId == "" {
		return errors.New("requestId is empty")
	}
	if status != GeneratedImageRequestStatusFailed && status != GeneratedImageRequestStatusStorageFailed {
		return errors.New("invalid generated image failure status")
	}
	result := DB.Model(&GeneratedImageRequest{}).
		Where("request_id = ? AND status = ?", requestId, GeneratedImageRequestStatusProcessing).
		Updates(map[string]interface{}{
			"status":       status,
			"error_status": errorStatus,
			"error_code":   errorCode,
			"updated_at":   common.GetTimestamp(),
		})
	return result.Error
}

func CreateGeneratedAssetsAndMarkStored(requestId string, assets []GeneratedAsset) error {
	if requestId == "" {
		return errors.New("requestId is empty")
	}
	if len(assets) == 0 {
		return errors.New("generated assets are empty")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var request GeneratedImageRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("request_id = ?", requestId).First(&request).Error; err != nil {
			return err
		}
		if request.Status == GeneratedImageRequestStatusStored || request.Status == GeneratedImageRequestStatusSucceeded {
			return nil
		}
		if request.Status != GeneratedImageRequestStatusProcessing {
			return errors.New("generated image request is not processing")
		}
		if err := tx.Create(&assets).Error; err != nil {
			return err
		}
		return tx.Model(&request).Updates(map[string]interface{}{
			"status":     GeneratedImageRequestStatusStored,
			"updated_at": common.GetTimestamp(),
		}).Error
	})
}

func MarkGeneratedImageRequestSucceeded(requestId string) error {
	if requestId == "" {
		return errors.New("requestId is empty")
	}
	return DB.Model(&GeneratedImageRequest{}).
		Where("request_id = ? AND status IN ?", requestId, []string{
			GeneratedImageRequestStatusStored,
			GeneratedImageRequestStatusSucceeded,
		}).
		Updates(map[string]interface{}{
			"status":     GeneratedImageRequestStatusSucceeded,
			"updated_at": common.GetTimestamp(),
		}).Error
}

func GetGeneratedAssetsByRequestId(requestId string) ([]GeneratedAsset, error) {
	if requestId == "" {
		return nil, errors.New("requestId is empty")
	}
	var assets []GeneratedAsset
	err := DB.Where("request_id = ?", requestId).Order("id asc").Find(&assets).Error
	return assets, err
}

func GetGeneratedAssetByTaskIdAndUser(taskId string, userId int) (*GeneratedAsset, error) {
	if taskId == "" || userId <= 0 {
		return nil, errors.New("invalid generated asset identity")
	}
	var asset GeneratedAsset
	if err := DB.Where("task_id = ? AND user_id = ?", taskId, userId).First(&asset).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

func ListGeneratedAssetsByUser(userId int, page int, pageSize int) ([]GeneratedAsset, int64, error) {
	if userId <= 0 {
		return nil, 0, errors.New("invalid userId")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	query := DB.Model(&GeneratedAsset{}).Where("user_id = ?", userId)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var assets []GeneratedAsset
	err := query.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&assets).Error
	return assets, total, err
}
