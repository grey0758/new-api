package model

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	// BackgroundTaskLeaderOption can temporarily pause shared background jobs.
	// Environment variable BACKGROUND_TASK_LEADER_ENABLED takes precedence.
	BackgroundTaskLeaderOption = "BackgroundTaskLeaderEnabled"

	backgroundTaskLeaseName                = "new-api-background-tasks-v1"
	defaultBackgroundTaskLeaseSeconds      = 45
	defaultBackgroundTaskHeartbeatSeconds  = 10
	minimumBackgroundTaskHeartbeatSeconds  = 2
	backgroundTaskLeaderHolderRandomLength = 8
	backgroundTaskLeaseQueryTimeout        = 5 * time.Second
)

// BackgroundTaskLease coordinates shared schedulers between processes that use
// the same application database. Unix timestamps keep the schema portable
// across SQLite, MySQL, and PostgreSQL.
type BackgroundTaskLease struct {
	Name       string `json:"name" gorm:"primaryKey;size:96"`
	HolderID   string `json:"-" gorm:"size:160;not null;default:''"`
	LeaseUntil int64  `json:"lease_until" gorm:"not null;default:0;index"`
	UpdatedAt  int64  `json:"updated_at" gorm:"not null;default:0"`
}

type backgroundTaskLeaseManager struct {
	db            *gorm.DB
	leaseName     string
	holderID      string
	leaseDuration time.Duration
}

var (
	backgroundTaskLeaderOnce     sync.Once
	backgroundTaskLeaderStopOnce sync.Once
	backgroundTaskLeader         atomic.Bool
	backgroundTaskLeaderManager  *backgroundTaskLeaseManager
	backgroundTaskLeaderStop     chan struct{}
	backgroundTaskLeaderDone     chan struct{}
)

func ensureBackgroundTaskLeaseRow(db *gorm.DB) error {
	lease := BackgroundTaskLease{Name: backgroundTaskLeaseName}
	return db.Where("name = ?", backgroundTaskLeaseName).FirstOrCreate(&lease).Error
}

func (m *backgroundTaskLeaseManager) refresh(now time.Time) (bool, error) {
	if m == nil || m.db == nil {
		return false, fmt.Errorf("background task lease manager is not initialized")
	}
	nowUnix := now.Unix()
	leaseUntil := now.Add(m.leaseDuration).Unix()
	ctx, cancel := context.WithTimeout(context.Background(), backgroundTaskLeaseQueryTimeout)
	defer cancel()
	result := m.db.WithContext(ctx).Model(&BackgroundTaskLease{}).
		Where("name = ? AND (holder_id = ? OR lease_until <= ?)", m.leaseName, m.holderID, nowUnix).
		Updates(map[string]interface{}{
			"holder_id":   m.holderID,
			"lease_until": leaseUntil,
			"updated_at":  nowUnix,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (m *backgroundTaskLeaseManager) release(now time.Time) error {
	if m == nil || m.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), backgroundTaskLeaseQueryTimeout)
	defer cancel()
	return m.db.WithContext(ctx).Model(&BackgroundTaskLease{}).
		Where("name = ? AND holder_id = ?", m.leaseName, m.holderID).
		Updates(map[string]interface{}{
			"holder_id":   "",
			"lease_until": int64(0),
			"updated_at":  now.Unix(),
		}).Error
}

func backgroundTaskLeaderEnabled() bool {
	if raw, ok := os.LookupEnv("BACKGROUND_TASK_LEADER_ENABLED"); ok && strings.TrimSpace(raw) != "" {
		enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			common.SysError(fmt.Sprintf("failed to parse BACKGROUND_TASK_LEADER_ENABLED: %v, using true", err))
			return true
		}
		return enabled
	}

	common.OptionMapRWMutex.RLock()
	raw := strings.TrimSpace(common.OptionMap[BackgroundTaskLeaderOption])
	common.OptionMapRWMutex.RUnlock()
	if raw == "" {
		return true
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		common.SysError(fmt.Sprintf("failed to parse %s: %v, using true", BackgroundTaskLeaderOption, err))
		return true
	}
	return enabled
}

func refreshBackgroundTaskLeadership(manager *backgroundTaskLeaseManager) {
	if !backgroundTaskLeaderEnabled() {
		wasLeader := backgroundTaskLeader.Swap(false)
		if err := manager.release(time.Now()); err != nil {
			common.SysError(fmt.Sprintf("background task leader release failed while disabled: %v", err))
		}
		if wasLeader {
			common.SysLog("background task leader released because scheduling is disabled")
		}
		return
	}

	isLeader, err := manager.refresh(time.Now())
	if err != nil {
		wasLeader := backgroundTaskLeader.Swap(false)
		if wasLeader {
			common.SysError(fmt.Sprintf("background task leader lost after lease refresh failure: %v", err))
		} else {
			common.SysError(fmt.Sprintf("background task leader lease refresh failed: %v", err))
		}
		return
	}

	wasLeader := backgroundTaskLeader.Swap(isLeader)
	if isLeader && !wasLeader {
		common.SysLog(fmt.Sprintf("background task leader acquired: holder=%s lease=%s", manager.holderID, manager.leaseDuration))
	} else if !isLeader && wasLeader {
		common.SysLog("background task leader lost; shared schedulers are paused on this process")
	}
}

// StartBackgroundTaskLeader starts one database-backed leader candidate per
// process. Shared jobs should call IsBackgroundTaskLeader before each run;
// process-local cache flushers must continue to run independently.
func StartBackgroundTaskLeader() {
	backgroundTaskLeaderOnce.Do(func() {
		if !common.IsMasterNode {
			common.SysLog("background task leader disabled on slave node")
			return
		}

		heartbeatSeconds := common.GetEnvOrDefault("BACKGROUND_TASK_LEADER_HEARTBEAT_SECONDS", defaultBackgroundTaskHeartbeatSeconds)
		if heartbeatSeconds < minimumBackgroundTaskHeartbeatSeconds {
			heartbeatSeconds = minimumBackgroundTaskHeartbeatSeconds
		}
		leaseSeconds := common.GetEnvOrDefault("BACKGROUND_TASK_LEADER_LEASE_SECONDS", defaultBackgroundTaskLeaseSeconds)
		if leaseSeconds <= heartbeatSeconds*2 {
			leaseSeconds = heartbeatSeconds * 3
		}

		hostname, err := os.Hostname()
		if err != nil || strings.TrimSpace(hostname) == "" {
			hostname = "unknown-host"
		}
		holderID := fmt.Sprintf("%s:%d:%s", hostname, os.Getpid(), common.GetRandomString(backgroundTaskLeaderHolderRandomLength))
		manager := &backgroundTaskLeaseManager{
			db:            DB,
			leaseName:     backgroundTaskLeaseName,
			holderID:      holderID,
			leaseDuration: time.Duration(leaseSeconds) * time.Second,
		}
		backgroundTaskLeaderManager = manager
		backgroundTaskLeaderStop = make(chan struct{})
		backgroundTaskLeaderDone = make(chan struct{})

		// Acquire synchronously so immediate startup tasks have a deterministic
		// leader decision before their goroutines begin.
		refreshBackgroundTaskLeadership(manager)

		go func() {
			defer close(backgroundTaskLeaderDone)
			ticker := time.NewTicker(time.Duration(heartbeatSeconds) * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					refreshBackgroundTaskLeadership(manager)
				case <-backgroundTaskLeaderStop:
					return
				}
			}
		}()
	})
}

// StopBackgroundTaskLeader releases the lease during graceful shutdown.
// Abrupt container exits remain safe because the lease expires automatically.
func StopBackgroundTaskLeader() {
	backgroundTaskLeaderStopOnce.Do(func() {
		if backgroundTaskLeaderStop == nil {
			return
		}
		close(backgroundTaskLeaderStop)
		<-backgroundTaskLeaderDone
		backgroundTaskLeader.Store(false)
		if err := backgroundTaskLeaderManager.release(time.Now()); err != nil {
			common.SysError(fmt.Sprintf("background task leader release failed: %v", err))
		}
	})
}

// IsBackgroundTaskLeader reports whether this process currently owns the
// shared scheduler lease.
func IsBackgroundTaskLeader() bool {
	return common.IsMasterNode && backgroundTaskLeader.Load()
}
