package service

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestImageJobCapacityConfiguration(t *testing.T) {
	c, err := LoadImageJobCapacity()
	require.NoError(t, err)
	require.Equal(t, int64(48), c.Limits.Pending)
	require.Equal(t, 1, c.FourKWorkers)
	for name, value := range map[string]string{
		"IMAGE_JOBS_MAX_PENDING": "1024", "IMAGE_JOBS_MAX_PENDING_PER_USER": "1024",
		"IMAGE_JOBS_WORKERS_STANDARD": "128", "IMAGE_JOBS_WORKERS_4K": "8",
		"IMAGE_JOBS_MAX_REQUEST_BYTES_TOTAL": "1073741824", "IMAGE_JOBS_MAX_RESULT_BYTES_TOTAL": "17179869184", "IMAGE_JOBS_MAX_RETAINED": "2500000",
	} {
		t.Setenv(name, value)
	}
	c, err = LoadImageJobCapacity()
	require.NoError(t, err)
	require.Equal(t, 128, c.StandardWorkers)
	require.Equal(t, 8, c.FourKWorkers)
	require.Equal(t, int64(1024), c.Limits.PerUser)
	for _, tc := range []struct{ name, value string }{
		{"IMAGE_JOBS_WORKERS_STANDARD", "0"}, {"IMAGE_JOBS_WORKERS_4K", "-1"},
		{"IMAGE_JOBS_WORKERS_STANDARD", "513"}, {"IMAGE_JOBS_MAX_PENDING", "invalid"},
		{"IMAGE_JOBS_MAX_PENDING", "100"}, {"IMAGE_JOBS_MAX_PENDING_PER_USER", "1025"},
		{"IMAGE_JOBS_MAX_REQUEST_BYTES_TOTAL", "1"}, {"IMAGE_JOBS_MAX_RESULT_BYTES_TOTAL", "999999999999999999999"},
	} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			_, err := LoadImageJobCapacity()
			require.Error(t, err)
		})
	}
}
