package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesStreamGuardRecordsByUserTokenModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		_ = getResponsesStreamGuardCache().Purge()
	})
	common.RedisEnabled = false
	require.NoError(t, getResponsesStreamGuardCache().Purge())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5"}`))
	ctx.Set("id", 181)
	ctx.Set("token_id", 9)

	require.False(t, ShouldGuardResponsesStream(ctx, "gpt-5"))

	RecordResponsesStreamGuard(ctx, "gpt-5")
	require.True(t, ShouldGuardResponsesStream(ctx, "gpt-5"))
	require.False(t, ShouldGuardResponsesStream(ctx, "gpt-4.1"))
}

func TestResponsesStreamGuardIgnoresNonResponsesRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		_ = getResponsesStreamGuardCache().Purge()
	})
	common.RedisEnabled = false
	require.NoError(t, getResponsesStreamGuardCache().Purge())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("id", 181)
	ctx.Set("token_id", 9)

	RecordResponsesStreamGuard(ctx, "gpt-5")
	require.False(t, ShouldGuardResponsesStream(ctx, "gpt-5"))
}
