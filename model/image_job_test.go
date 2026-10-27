package model

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func imageJobTestDB(t *testing.T) {
	t.Helper()
	previous := DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:jobs_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	DB = db
	require.NoError(t, InitImageJobs())
	t.Cleanup(func() { DB = previous; _ = conn.Close() })
}
func testImageJob(id string, user int) *ImageJob {
	return &ImageJob{ID: id, UserID: user, TokenID: user, KeyHash: id, RequestHash: "same", RequestID: id, Model: "fixture", Family: "4k", Status: "queued", RequestBody: []byte("original"), RequestBytes: 8, CreatedAt: 100, QueueExpiresAt: 200, ExpiresAt: 1000}
}

var testJobLimits = ImageJobLimits{Pending: 48, PerUser: 8, PendingBytes: 1024, StoredBytes: 1024}

func TestImageJobAtomicIdempotencyAndOwnership(t *testing.T) {
	imageJobTestDB(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, fresh, err := AdmitImageJob(testImageJob("a", 1), testJobLimits)
			require.NoError(t, err)
			if fresh {
				mu.Lock()
				created++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, created)
	conflict := testImageJob("a", 1)
	conflict.RequestHash = "different"
	_, _, err := AdmitImageJob(conflict, testJobLimits)
	require.ErrorIs(t, err, ErrImageJobConflict)
	_, err = FindImageJob(2, "a")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	job, err := ClaimImageJob("first", "4k", 101)
	require.NoError(t, err)
	require.Equal(t, "a", job.ID)
	_, err = ClaimImageJob("second", "4k", 101)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.ErrorIs(t, UpdateOwnedImageJob("a", "wrong", map[string]any{"provider_started": true}), ErrImageJobLost)
}

func TestImageJobBoundsAndResultImmutability(t *testing.T) {
	for _, tc := range []struct {
		name       string
		limits     ImageJobLimits
		secondUser int
	}{
		{"count", ImageJobLimits{Pending: 1, PerUser: 8, PendingBytes: 1024, StoredBytes: 1024}, 2},
		{"user", ImageJobLimits{Pending: 48, PerUser: 1, PendingBytes: 1024, StoredBytes: 1024}, 1},
		{"bytes", ImageJobLimits{Pending: 48, PerUser: 8, PendingBytes: 12, StoredBytes: 1024}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imageJobTestDB(t)
			_, _, err := AdmitImageJob(testImageJob("a", 1), tc.limits)
			require.NoError(t, err)
			_, _, err = AdmitImageJob(testImageJob("b", tc.secondUser), tc.limits)
			require.ErrorIs(t, err, ErrImageJobFull)
		})
	}
	t.Run("immutable", func(t *testing.T) {
		imageJobTestDB(t)
		_, _, err := AdmitImageJob(testImageJob("a", 1), testJobLimits)
		require.NoError(t, err)
		_, err = ClaimImageJob("first", "4k", 101)
		require.NoError(t, err)
		require.NoError(t, CheckpointImageJobResult("a", "first", "hash", []byte("full response"), 400))
		require.NoError(t, CheckpointImageJobResult("a", "first", "hash", []byte("full response"), 400))
		require.ErrorIs(t, CheckpointImageJobResult("a", "first", "other", []byte("replacement"), 400), ErrImageJobConflict)
		job, err := FindImageJob(1, "a")
		require.NoError(t, err)
		require.Equal(t, "full response", string(job.ResultBody))
		limits := testJobLimits
		limits.StoredBytes = 10
		_, _, err = AdmitImageJob(testImageJob("b", 2), limits)
		require.ErrorIs(t, err, ErrImageJobFull)
	})
}

func TestImageJobRestartExpiryAndCancelNeverReplay(t *testing.T) {
	imageJobTestDB(t)
	for _, id := range []string{"running", "queued", "cancelled", "expired"} {
		job := testImageJob(id, 1)
		if id == "expired" {
			job.QueueExpiresAt = 100
		}
		_, _, err := AdmitImageJob(job, testJobLimits)
		require.NoError(t, err)
	}
	running, err := ClaimImageJob("old", "4k", 101)
	require.NoError(t, err)
	// Whichever oldest ID was claimed, owner loss makes it terminal for claims.
	require.NoError(t, MaintainImageJobs("new", true, 102))
	job, err := FindImageJob(1, running.ID)
	require.NoError(t, err)
	require.Equal(t, "reconciliation_required", job.Status)
	require.Empty(t, job.RequestBody)
	cancelID := "queued"
	if running.ID == cancelID {
		cancelID = "running"
	}
	ok, err := CancelImageJob(1, cancelID)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = CancelImageJob(1, running.ID)
	require.NoError(t, err)
	require.False(t, ok)
	for {
		next, err := ClaimImageJob("new", "4k", 103)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			break
		}
		require.NoError(t, err)
		require.NotEqual(t, running.ID, next.ID)
		require.NotEqual(t, cancelID, next.ID)
		require.NotEqual(t, "expired", next.ID)
	}
	// An old in-flight owner may still checkpoint its already-produced result;
	// that is recovery of delivery, not a second provider invocation.
	require.NoError(t, CheckpointImageJobResult(running.ID, "old", "result", []byte("result"), 150))
	require.NoError(t, MaintainImageJobs("new", false, 151))
	job, err = FindImageJob(1, running.ID)
	require.NoError(t, err)
	require.Empty(t, job.ResultBody)
	require.Equal(t, "result", job.ResultHash)
}

func TestImageJobCapacityReasonsAndRetainedOverride(t *testing.T) {
	for _, tc := range []struct {
		reason string
		limits ImageJobLimits
		user   int
	}{
		{"user_pending", ImageJobLimits{Pending: 2, PerUser: 1, PendingBytes: 100, StoredBytes: 100}, 1},
		{"global_pending", ImageJobLimits{Pending: 1, PerUser: 2, PendingBytes: 100, StoredBytes: 100}, 2},
		{"request_bytes", ImageJobLimits{Pending: 2, PerUser: 2, PendingBytes: 8, StoredBytes: 100}, 2},
		{"retained_jobs", ImageJobLimits{Pending: 2, PerUser: 2, PendingBytes: 100, StoredBytes: 100, Retained: 1}, 2},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			imageJobTestDB(t)
			_, _, err := AdmitImageJob(testImageJob("first", 1), tc.limits)
			require.NoError(t, err)
			_, _, err = AdmitImageJob(testImageJob("second", tc.user), tc.limits)
			require.ErrorIs(t, err, ErrImageJobFull)
			var capacity *ImageJobCapacityError
			require.ErrorAs(t, err, &capacity)
			require.Equal(t, tc.reason, capacity.Reason)
			_, fresh, err := AdmitImageJob(testImageJob("first", 1), tc.limits)
			require.NoError(t, err)
			require.False(t, fresh)
		})
	}
	imageJobTestDB(t)
	require.NoError(t, DB.Exec("INSERT INTO image_jobs (id,user_id,token_id,key_hash,request_hash,request_id,model,family,status,client_ip,request_bytes,created_at,queue_expires_at,expires_at) VALUES ('retained',1,1,'retained','hash','id','fixture','4k','completed','',0,1,1,9999999999)").Error)
	limits := ImageJobLimits{Pending: 2, PerUser: 2, PendingBytes: 100, StoredBytes: 100, Retained: 2}
	_, fresh, err := AdmitImageJob(testImageJob("new", 1), limits)
	require.NoError(t, err)
	require.True(t, fresh)
}
