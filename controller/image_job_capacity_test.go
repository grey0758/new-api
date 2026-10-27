package controller

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

// Real auth, quote and durable admission; the upstream must never be called
// while queued. Burst 200 unique requests from the same tt-api-shaped user.
func TestImageJobCapacity200RequestBurstAndRecovery(t *testing.T) {
	router, _, token := imageJobFixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("queued admission contacted upstream") }))
	previous := imageJobCapacity
	imageJobCapacity = service.ImageJobCapacity{Limits: model.ImageJobLimits{Pending: 200, PerUser: 200, PendingBytes: 1 << 30, StoredBytes: 16 << 30, Retained: 2500000}, StandardWorkers: 128, FourKWorkers: 8}
	t.Cleanup(func() { imageJobCapacity = previous })
	start := time.Now()
	var wg sync.WaitGroup
	codes := make(chan int, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := jobRequest(router, token, "POST", "/v1/images/generations", `{"model":"fixture-4k","prompt":"capacity fixture","n":1}`, fmt.Sprintf("capacity-%d", i), true)
			codes <- w.Code
		}(i)
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		require.Equal(t, 202, code)
	}
	require.Less(t, time.Since(start), time.Minute)
	w := jobRequest(router, token, "POST", "/v1/images/generations", `{"model":"fixture-4k","prompt":"capacity fixture","n":1}`, "capacity-overflow", true)
	require.Equal(t, 429, w.Code)
	require.Equal(t, "global_pending", w.Header().Get("X-Image-Queue-Limit"))
	w = jobRequest(router, token, "POST", "/v1/images/generations", `{"model":"fixture-4k","prompt":"capacity fixture","n":1}`, "capacity-0", true)
	require.Equal(t, 202, w.Code)
	require.Equal(t, "true", w.Header().Get("X-Idempotent-Replay"))
	var count int64
	require.NoError(t, model.DB.Model(&model.ImageJob{}).Count(&count).Error)
	require.Equal(t, int64(200), count)
	var first model.ImageJob
	require.NoError(t, model.DB.First(&first).Error)
	ok, err := model.CancelImageJob(first.UserID, first.ID)
	require.NoError(t, err)
	require.True(t, ok)
	w = jobRequest(router, token, "POST", "/v1/images/generations", `{"model":"fixture-4k","prompt":"capacity fixture","n":1}`, "capacity-overflow", true)
	require.Equal(t, 202, w.Code)
	t.Logf("200 authenticated admissions in %s; overflow bounded; replay and released capacity preserved", time.Since(start))
}

// Exercise the actual worker launcher with durable claims and a blocked fake
// execution, showing both family limits and no work from a nonleader.
func TestImageJobExpandedWorkerBoundsAndSingleClaim(t *testing.T) {
	_, _, _ = imageJobFixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for i := 0; i < 200; i++ {
		family := "standard"
		if i >= 160 {
			family = "4k"
		}
		job := &model.ImageJob{ID: fmt.Sprintf("expanded-%03d", i), UserID: 1, TokenID: 1, KeyHash: fmt.Sprintf("key-%d", i), RequestHash: "fixture", Family: family, Status: "queued", QueueExpiresAt: time.Now().Add(time.Minute).Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix()}
		require.NoError(t, model.DB.Create(job).Error)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var leader atomic.Bool
	var standard, fourK atomic.Int32
	entered := make(chan string, 200)
	release := make(chan struct{})
	var lock sync.Mutex
	seen := map[string]bool{}
	var callbacks sync.WaitGroup
	startImageJobWorkers(ctx, service.ImageJobCapacity{StandardWorkers: 128, FourKWorkers: 8}, time.Millisecond, leader.Load, func(family string) {
		callbacks.Add(1)
		defer callbacks.Done()
		job, err := model.ClaimImageJob("capacity-owner", family, time.Now().Unix())
		if err != nil {
			return
		}
		lock.Lock()
		if seen[job.ID] {
			t.Error("duplicate execution claim")
		}
		seen[job.ID] = true
		lock.Unlock()
		if family == "4k" {
			fourK.Add(1)
		} else {
			standard.Add(1)
		}
		entered <- family
		<-release
	})
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, len(entered))
	leader.Store(true)
	for i := 0; i < 136; i++ {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			cancel()
			close(release)
			callbacks.Wait()
			t.Fatal("expanded workers did not start")
		}
	}
	require.Equal(t, int32(128), standard.Load())
	require.Equal(t, int32(8), fourK.Load())
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, len(entered), "pool exceeded configured concurrency")
	cancel()
	close(release)
	callbacks.Wait()
	require.Len(t, seen, 136)
	t.Log("128 standard + 8 fourK durable claims concurrently; unique and bounded")
}
