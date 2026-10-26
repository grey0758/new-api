package model

import (
	"bytes"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Opt-in dedicated EMPTY fixture databases, never the application's SQL_DSN.
// Exercises real cross-process database locking and >64KiB binary storage.
func TestImageJobExternalDatabase(t *testing.T) {
	dsn := os.Getenv("IMAGE_JOBS_FIXTURE_DSN")
	if dsn == "" {
		t.Skip("dedicated fixture database not configured")
	}
	var driver gorm.Dialector
	oldMySQL, oldPG, oldSQLite := common.UsingMySQL, common.UsingPostgreSQL, common.UsingSQLite
	if os.Getenv("IMAGE_JOBS_FIXTURE_DRIVER") == "postgres" {
		driver = postgres.Open(dsn)
		common.UsingPostgreSQL = true
		common.UsingMySQL = false
	} else {
		driver = mysql.Open(dsn)
		common.UsingMySQL = true
		common.UsingPostgreSQL = false
	}
	common.UsingSQLite = false
	t.Cleanup(func() {
		common.UsingMySQL, common.UsingPostgreSQL, common.UsingSQLite = oldMySQL, oldPG, oldSQLite
		initCol()
	})
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	old := DB
	DB = db
	t.Cleanup(func() { DB = old })
	require.False(t, db.Migrator().HasTable(&ImageJob{}), "fixture must be a new dedicated database")
	require.NoError(t, InitImageJobs())
	limits := ImageJobLimits{Pending: 48, PerUser: 8, PendingBytes: 32 << 20, StoredBytes: 32 << 20}
	var wg sync.WaitGroup
	var count atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, created, err := AdmitImageJob(testImageJob("parallel", 1), limits)
			require.NoError(t, err)
			if created {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), count.Load())
	job, err := ClaimImageJob("one", "4k", 101)
	require.NoError(t, err)
	require.NoError(t, UpdateOwnedImageJob(job.ID, "one", map[string]any{"provider_started": true, "billing_status": "reserved"}))
	body := bytes.Repeat([]byte("fixture"), 200000)
	require.NoError(t, CheckpointImageJobResult(job.ID, "one", "hash", body, 400))
	retrieved, err := FindImageJob(1, job.ID)
	require.NoError(t, err)
	require.Equal(t, body, retrieved.ResultBody)
	require.NoError(t, MaintainImageJobs("two", true, 102))
	retrieved, err = FindImageJob(1, job.ID)
	require.NoError(t, err)
	require.Equal(t, "reconciliation_required", retrieved.Status)
	_, err = ClaimImageJob("two", "4k", 103)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NoError(t, MaintainImageJobs("two", false, 401))
	retrieved, err = FindImageJob(1, job.ID)
	require.NoError(t, err)
	require.Empty(t, retrieved.ResultBody)
}
