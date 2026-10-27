package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ImageJob is a durable outer execution ledger. Running jobs are NEVER put back
// in the queue after owner loss: a provider POST may already have been accepted.
// Binary columns map to bytea/blob/longblob rather than MySQL's 64 KiB TEXT.
type ImageJob struct {
	ID              string `gorm:"primaryKey;size:64"`
	UserID          int    `gorm:"uniqueIndex:uid_image_job_key;index;not null"`
	TokenID         int    `gorm:"index;not null"`
	KeyHash         string `gorm:"uniqueIndex:uid_image_job_key;size:64;not null"`
	RequestHash     string `gorm:"size:64;not null"`
	RequestID       string `gorm:"size:96;not null"`
	Model           string `gorm:"size:256;not null"`
	Family          string `gorm:"size:16;index;not null"`
	Status          string `gorm:"size:32;index;not null"`
	Owner           string `gorm:"size:96;not null;default:''"`
	ClientIP        string `gorm:"size:64;not null"`
	TokenSuffix     string `gorm:"size:32;not null;default:''"`
	RequestBody     []byte
	RequestBytes    int64 `gorm:"not null"`
	ResultBody      []byte
	ResultHash      string `gorm:"size:64;not null;default:''"`
	ResultBytes     int64  `gorm:"not null;default:0"`
	HTTPStatus      int    `gorm:"not null;default:0"`
	ErrorCode       string `gorm:"size:96;not null;default:''"`
	BillingStatus   string `gorm:"size:32;not null;default:'not_started'"`
	ProviderStarted bool   `gorm:"not null;default:false"`
	CreatedAt       int64  `gorm:"index;not null"`
	StartedAt       int64  `gorm:"not null;default:0"`
	FinishedAt      int64  `gorm:"not null;default:0"`
	QueueExpiresAt  int64  `gorm:"not null"`
	ExpiresAt       int64  `gorm:"index;not null"`
	ResultExpiresAt int64  `gorm:"not null;default:0"`
}

// All admission and claiming transactions serialize through this row, including
// aggregate count/byte checks. A SELECT FOR UPDATE alone has no SQLite effect;
// the UPDATE is deliberate and obtains a write lock on all supported databases.
type ImageJobGate struct {
	ID     int   `gorm:"primaryKey"`
	Serial int64 `gorm:"not null;default:0"`
}

type ImageJobLimits struct {
	Pending, PerUser, Retained int64
	PendingBytes, StoredBytes  int64
}

var ErrImageJobConflict = errors.New("idempotency_key_conflict")
var ErrImageJobFull = errors.New("image_job_queue_full")

// Capacity errors retain the public error identity and contain counts only.
type ImageJobCapacityError struct {
	Reason         string
	Current, Limit int64
}

func (e *ImageJobCapacityError) Error() string { return ErrImageJobFull.Error() }
func (e *ImageJobCapacityError) Unwrap() error { return ErrImageJobFull }

var ErrImageJobLost = errors.New("image_job_owner_lost")

func imageJobDB() *gorm.DB {
	return DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
}

func InitImageJobs() error {
	if commonKeyCol == "" {
		initCol()
	}
	if err := imageJobDB().AutoMigrate(&ImageJob{}, &ImageJobGate{}); err != nil {
		return err
	}
	return imageJobDB().Where("id = ?", 1).FirstOrCreate(&ImageJobGate{ID: 1}).Error
}

func lockImageJobs(tx *gorm.DB) error {
	return tx.Model(&ImageJobGate{}).Where("id = ?", 1).UpdateColumn("serial", gorm.Expr("serial + 1")).Error
}

func FindImageJob(userID int, id string) (*ImageJob, error) {
	var job ImageJob
	err := imageJobDB().Where("id = ? AND user_id = ?", id, userID).First(&job).Error
	return &job, err
}

func AdmitImageJob(job *ImageJob, limits ImageJobLimits) (*ImageJob, bool, error) {
	created := false
	err := imageJobDB().Transaction(func(tx *gorm.DB) error {
		if err := lockImageJobs(tx); err != nil {
			return err
		}
		var existing ImageJob
		err := tx.Where("user_id = ? AND key_hash = ?", job.UserID, job.KeyHash).First(&existing).Error
		if err == nil {
			if existing.RequestHash != job.RequestHash || existing.TokenID != job.TokenID {
				return ErrImageJobConflict
			}
			*job = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		pending := tx.Model(&ImageJob{}).Where("status IN ?", []string{"queued", "running"})
		var count, own, retained int64
		if err := pending.Count(&count).Error; err != nil {
			return err
		}
		if err := tx.Model(&ImageJob{}).Where("status IN ? AND user_id = ?", []string{"queued", "running"}, job.UserID).Count(&own).Error; err != nil {
			return err
		}
		var sizes struct {
			PendingBytes int64
			StoredBytes  int64
		}
		if err := tx.Model(&ImageJob{}).Select("COALESCE(SUM(request_bytes),0) AS pending_bytes, COALESCE(SUM(result_bytes),0) AS stored_bytes").Scan(&sizes).Error; err != nil {
			return err
		}
		if err := tx.Model(&ImageJob{}).Count(&retained).Error; err != nil {
			return err
		}
		maxRetained := limits.Retained
		if maxRetained <= 0 {
			maxRetained = 10000
		}
		for _, bound := range []struct {
			reason         string
			current, limit int64
		}{
			{"global_pending", count, limits.Pending},
			{"user_pending", own, limits.PerUser},
			{"result_bytes", sizes.StoredBytes, limits.StoredBytes},
			{"retained_jobs", retained, maxRetained},
		} {
			if bound.current >= bound.limit {
				return &ImageJobCapacityError{Reason: bound.reason, Current: bound.current, Limit: bound.limit}
			}
		}
		if sizes.PendingBytes+job.RequestBytes > limits.PendingBytes {
			return &ImageJobCapacityError{Reason: "request_bytes", Current: sizes.PendingBytes + job.RequestBytes, Limit: limits.PendingBytes}
		}
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return job, created, err
}

// ClaimImageJob is CAS-protected independently of the leader lease. Even if
// leadership changes mid-call, no second worker may execute this job.
func ClaimImageJob(owner, family string, now int64) (*ImageJob, error) {
	// Empty queues must not generate continuous UPDATE/binlog traffic.
	var waiting int64
	if err := imageJobDB().Model(&ImageJob{}).Where("status = ? AND family = ? AND queue_expires_at > ?", "queued", family, now).Limit(1).Count(&waiting).Error; err != nil {
		return nil, err
	}
	if waiting == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var job ImageJob
	err := imageJobDB().Transaction(func(tx *gorm.DB) error {
		if err := lockImageJobs(tx); err != nil {
			return err
		}
		if err := tx.Where("status = ? AND family = ? AND queue_expires_at > ?", "queued", family, now).Order("created_at ASC, id ASC").First(&job).Error; err != nil {
			return err
		}
		res := tx.Model(&ImageJob{}).Where("id = ? AND status = ?", job.ID, "queued").Updates(map[string]any{"status": "running", "owner": owner, "started_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrImageJobLost
		}
		job.Status = "running"
		job.Owner = owner
		job.StartedAt = now
		return nil
	})
	return &job, err
}

func UpdateOwnedImageJob(id, owner string, values map[string]any) error {
	res := imageJobDB().Model(&ImageJob{}).Where("id = ? AND owner = ? AND status IN ?", id, owner, []string{"running", "reconciliation_required"}).Updates(values)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrImageJobLost
	}
	return nil
}

func CheckpointImageJobResult(id, owner, hash string, body []byte, expires int64) error {
	return imageJobDB().Transaction(func(tx *gorm.DB) error {
		// One immutable response per job; retries below are DB-only, never Google.
		res := tx.Model(&ImageJob{}).Where("id = ? AND owner = ? AND status IN ? AND result_hash = ?", id, owner, []string{"running", "reconciliation_required"}, "").Updates(map[string]any{"result_body": body, "result_hash": hash, "result_bytes": len(body), "http_status": 200, "result_expires_at": expires})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 1 {
			return nil
		}
		var job ImageJob
		if err := tx.Where("id = ? AND owner = ?", id, owner).First(&job).Error; err != nil {
			return err
		}
		if job.ResultHash == hash {
			return nil
		}
		return ErrImageJobConflict
	})
}

func MaintainImageJobs(owner string, recoverLost bool, now int64) error {
	var changes int64
	query := imageJobDB().Model(&ImageJob{}).Where("(status = ? AND queue_expires_at <= ?) OR (result_bytes > ? AND result_expires_at <= ?) OR (expires_at <= ? AND status NOT IN ?)", "queued", now, 0, now, now, []string{"running", "reconciliation_required"})
	if recoverLost {
		query = query.Or("status = ? AND owner <> ?", "running", owner)
	}
	if err := query.Count(&changes).Error; err != nil {
		return err
	}
	if changes == 0 {
		return nil
	}
	return imageJobDB().Transaction(func(tx *gorm.DB) error {
		if err := lockImageJobs(tx); err != nil {
			return err
		}
		if recoverLost {
			if err := tx.Model(&ImageJob{}).Where("status = ? AND owner <> ?", "running", owner).Updates(map[string]any{"status": "reconciliation_required", "error_code": "executor_interrupted", "request_body": nil, "request_bytes": 0, "finished_at": now}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&ImageJob{}).Where("status = ? AND queue_expires_at <= ?", "queued", now).Updates(map[string]any{"status": "expired", "error_code": "queue_expired", "request_body": nil, "request_bytes": 0, "finished_at": now}).Error; err != nil {
			return err
		}
		// Keep idempotency tombstones for seven days. URL bodies expire sooner;
		// expiry is explicit and never causes provider regeneration.
		if err := tx.Model(&ImageJob{}).Where("result_bytes > ? AND result_expires_at > ? AND result_expires_at <= ?", 0, 0, now).Updates(map[string]any{"result_body": nil, "result_bytes": 0}).Error; err != nil {
			return err
		}
		// Unresolved submitted work survives ordinary retention for manual review.
		return tx.Where("expires_at <= ? AND status NOT IN ?", now, []string{"running", "reconciliation_required"}).Delete(&ImageJob{}).Error
	})
}

func CancelImageJob(userID int, id string) (bool, error) {
	res := imageJobDB().Model(&ImageJob{}).Where("id = ? AND user_id = ? AND status = ?", id, userID, "queued").Updates(map[string]any{"status": "cancelled", "request_body": nil, "request_bytes": 0, "finished_at": time.Now().Unix()})
	return res.RowsAffected == 1, res.Error
}

func NewImageJobID() string { return "imgjob_" + common.GetUUID() }

func FindImageJobByKey(userID int, hash string) (*ImageJob, error) {
	var job ImageJob
	err := imageJobDB().Omit("request_body", "result_body").Where("user_id = ? AND key_hash = ?", userID, hash).First(&job).Error
	return &job, err
}

func FindImageJobMetadata(userID int, id string) (*ImageJob, error) {
	var job ImageJob
	err := imageJobDB().Omit("request_body", "result_body").Where("id = ? AND user_id = ?", id, userID).First(&job).Error
	return &job, err
}
