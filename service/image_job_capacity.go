package service

import (
	"fmt"
	"os"
	"strconv"

	"github.com/QuantumNous/new-api/model"
)

// Capacity is read once before the HTTP server starts. Values are finite and
// invalid configuration prevents startup rather than silently removing a guard.
type ImageJobCapacity struct {
	Limits                        model.ImageJobLimits
	StandardWorkers, FourKWorkers int
}

func LoadImageJobCapacity() (ImageJobCapacity, error) {
	c := ImageJobCapacity{Limits: model.ImageJobLimits{Pending: 48, PerUser: 8, PendingBytes: 128 << 20, StoredBytes: 1 << 30, Retained: 10000}, StandardWorkers: 4, FourKWorkers: 1}
	standard, fourK := int64(c.StandardWorkers), int64(c.FourKWorkers)
	fields := []struct {
		name     string
		dst      *int64
		min, max int64
	}{
		{"IMAGE_JOBS_MAX_PENDING", &c.Limits.Pending, 1, 10000},
		{"IMAGE_JOBS_MAX_PENDING_PER_USER", &c.Limits.PerUser, 1, 10000},
		{"IMAGE_JOBS_MAX_REQUEST_BYTES_TOTAL", &c.Limits.PendingBytes, ImageJobMaxRequestBytes, 8 << 30},
		{"IMAGE_JOBS_MAX_RESULT_BYTES_TOTAL", &c.Limits.StoredBytes, ImageJobMaxResultBytes, 64 << 30},
		{"IMAGE_JOBS_MAX_RETAINED", &c.Limits.Retained, 1, 3000000},
		{"IMAGE_JOBS_WORKERS_STANDARD", &standard, 1, 512},
		{"IMAGE_JOBS_WORKERS_4K", &fourK, 1, 64},
	}
	for _, f := range fields {
		if raw, ok := os.LookupEnv(f.name); ok {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n < f.min || n > f.max {
				return c, fmt.Errorf("%s must be an integer in [%d,%d]", f.name, f.min, f.max)
			}
			*f.dst = n
		}
	}
	if c.Limits.PerUser > c.Limits.Pending {
		return c, fmt.Errorf("IMAGE_JOBS_MAX_PENDING_PER_USER exceeds IMAGE_JOBS_MAX_PENDING")
	}
	if standard+fourK > c.Limits.Pending {
		return c, fmt.Errorf("image job worker count exceeds pending capacity")
	}
	c.StandardWorkers, c.FourKWorkers = int(standard), int(fourK)
	return c, nil
}
