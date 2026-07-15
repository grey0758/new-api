package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newBackgroundTaskLeaderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:background_task_leader_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&BackgroundTaskLease{}); err != nil {
		t.Fatalf("migrate background task lease: %v", err)
	}
	if err := ensureBackgroundTaskLeaseRow(db); err != nil {
		t.Fatalf("seed background task lease: %v", err)
	}
	return db
}

func TestBackgroundTaskLeaseAllowsOnlyOneLeaderAndFailsOver(t *testing.T) {
	db := newBackgroundTaskLeaderTestDB(t)
	now := time.Unix(1_700_000_000, 0)
	first := &backgroundTaskLeaseManager{
		db:            db,
		leaseName:     backgroundTaskLeaseName,
		holderID:      "first",
		leaseDuration: 30 * time.Second,
	}
	second := &backgroundTaskLeaseManager{
		db:            db,
		leaseName:     backgroundTaskLeaseName,
		holderID:      "second",
		leaseDuration: 30 * time.Second,
	}

	leader, err := first.refresh(now)
	if err != nil || !leader {
		t.Fatalf("first candidate should acquire lease: leader=%v err=%v", leader, err)
	}
	leader, err = second.refresh(now.Add(10 * time.Second))
	if err != nil {
		t.Fatalf("second candidate refresh: %v", err)
	}
	if leader {
		t.Fatal("second candidate acquired an unexpired lease")
	}

	leader, err = second.refresh(now.Add(31 * time.Second))
	if err != nil || !leader {
		t.Fatalf("second candidate should acquire expired lease: leader=%v err=%v", leader, err)
	}
	leader, err = first.refresh(now.Add(32 * time.Second))
	if err != nil {
		t.Fatalf("first candidate refresh after failover: %v", err)
	}
	if leader {
		t.Fatal("former leader reacquired a lease owned by the second candidate")
	}
}

func TestBackgroundTaskLeaseReleaseAllowsImmediateHandoff(t *testing.T) {
	db := newBackgroundTaskLeaderTestDB(t)
	now := time.Unix(1_700_000_000, 0)
	first := &backgroundTaskLeaseManager{db: db, leaseName: backgroundTaskLeaseName, holderID: "first", leaseDuration: time.Minute}
	second := &backgroundTaskLeaseManager{db: db, leaseName: backgroundTaskLeaseName, holderID: "second", leaseDuration: time.Minute}

	if leader, err := first.refresh(now); err != nil || !leader {
		t.Fatalf("first candidate should acquire lease: leader=%v err=%v", leader, err)
	}
	if err := first.release(now.Add(time.Second)); err != nil {
		t.Fatalf("release first lease: %v", err)
	}
	if leader, err := second.refresh(now.Add(2 * time.Second)); err != nil || !leader {
		t.Fatalf("second candidate should acquire released lease: leader=%v err=%v", leader, err)
	}
}

func TestBackgroundTaskLeaderEnabledUsesDatabaseOptionWithEnvironmentOverride(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{BackgroundTaskLeaderOption: "false"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})

	t.Setenv("BACKGROUND_TASK_LEADER_ENABLED", "")
	if backgroundTaskLeaderEnabled() {
		t.Fatal("database option should pause background task leadership")
	}

	t.Setenv("BACKGROUND_TASK_LEADER_ENABLED", "true")
	if !backgroundTaskLeaderEnabled() {
		t.Fatal("environment variable should override the database pause option")
	}
}
