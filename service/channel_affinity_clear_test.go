package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClearCurrentChannelAffinityDeletesCachedEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		_ = getChannelAffinityCache().Purge()
	})
	common.RedisEnabled = false

	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL("test-stale-affinity", 17, time.Minute))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	setChannelAffinityContext(ctx, channelAffinityMeta{
		CacheKey:   "test-stale-affinity",
		TTLSeconds: 60,
	})
	ctx.Set(ginKeyChannelAffinitySkipRetry, true)

	require.True(t, ClearCurrentChannelAffinity(ctx))

	_, found, err := cache.Get("test-stale-affinity")
	require.NoError(t, err)
	require.False(t, found)
	require.False(t, ShouldSkipRetryAfterChannelAffinityFailure(ctx))
}
