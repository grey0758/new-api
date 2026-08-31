package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayRetryableErrorExcludesFailedChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	retryParam := &service.RetryParam{}
	err := types.NewErrorWithStatusCode(errors.New("upstream 500"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)

	require.True(t, shouldRetry(c, err, 1, 7))

	retryParam.ExcludeChannel(7)
	require.True(t, retryParam.ExcludedChannelIds[7])
}

func TestRelaySkipRetryErrorReturnsDirectly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	err := types.NewErrorWithStatusCode(errors.New("bad request"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())

	require.False(t, shouldRetry(c, err, 1, 7))
}

func TestRejectOversizedResponsesCompactionBeforeBodyRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses/compact",
		strings.NewReader(`{"model":"gpt-5.6-sol","input":"small"}`),
	)
	c.Request.ContentLength = responsesCompactMaxRequestBodyBytes + 1

	err := rejectOversizedResponsesCompaction(c, types.RelayFormatOpenAIResponsesCompaction)

	require.NotNil(t, err)
	require.Equal(t, http.StatusRequestEntityTooLarge, err.StatusCode)
	require.True(t, types.IsSkipRetryError(err))
}

func TestRelayAffinitySkipAllowsRetryForAutoDisableError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalAutomaticDisable := common.AutomaticDisableChannelEnabled
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = originalAutomaticDisable
	})
	common.AutomaticDisableChannelEnabled = true

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("channel_affinity_skip_retry_on_failure", true)
	err := types.NewErrorWithStatusCode(
		errors.New("Your authentication token has been invalidated. Please try signing in again."),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusUnauthorized,
	)

	require.True(t, shouldRetry(c, err, 1, 7))
}

func TestRelayAffinitySkipAllowsTransientFailoverBeforeCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("channel_affinity_skip_retry_on_failure", true)
	err := types.NewErrorWithStatusCode(
		errors.New("upstream quota exhausted"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	require.True(t, shouldRetry(c, err, 1, 26))
}

func TestRelayAffinitySkipAllowsBalanceFailoverBeforeCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("channel_affinity_skip_retry_on_failure", true)
	err := types.NewErrorWithStatusCode(
		errors.New("Insufficient account balance"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)

	require.True(t, service.IsTransientRelayFailoverError(err))
	require.True(t, shouldRetry(c, err, 1, 32))
}

func TestRelayAffinitySkipAllowsRetryAfterChannelCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalCooldownEnabled := common.AutomaticChannelCooldownEnabled
	originalCooldownThreshold := common.ChannelCooldownFailureThreshold
	originalCooldownWindow := common.ChannelCooldownFailureWindowSeconds
	originalCooldownSeconds := common.ChannelCooldownSeconds
	t.Cleanup(func() {
		common.AutomaticChannelCooldownEnabled = originalCooldownEnabled
		common.ChannelCooldownFailureThreshold = originalCooldownThreshold
		common.ChannelCooldownFailureWindowSeconds = originalCooldownWindow
		common.ChannelCooldownSeconds = originalCooldownSeconds
	})
	common.AutomaticChannelCooldownEnabled = true
	common.ChannelCooldownFailureThreshold = 1
	common.ChannelCooldownFailureWindowSeconds = 120
	common.ChannelCooldownSeconds = 300

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("channel_affinity_skip_retry_on_failure", true)
	err := types.NewErrorWithStatusCode(
		errors.New("upstream quota exhausted"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)
	service.RecordChannelFailureForCooldown(*types.NewChannelError(126, 1, "cooling-test", false, "test-key", false), err)

	require.True(t, shouldRetry(c, err, 1, 126))
}

func TestRelayAffinitySkipAllowsRetryForTransientCredentialCooldown429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("channel_affinity_skip_retry_on_failure", true)
	err := types.NewErrorWithStatusCode(
		errors.New("All credentials for model gpt-5.5 are cooling down via provider codex"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	)

	require.True(t, service.IsTransientCredentialCooldownError(err))
	require.True(t, shouldRetry(c, err, 1, 1))
}

func TestRelayAffinitySkipAllowsRetryForTransientQuotaStyle400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("channel_affinity_skip_retry_on_failure", true)
	err := types.NewErrorWithStatusCode(
		errors.New("400 Bad Request: 号池额度已耗尽正在切换号池，请重试"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)

	require.True(t, service.IsTransientRelayFailoverError(err))
	require.True(t, shouldRetry(c, err, 1, 1))
}

func TestRelayStartsSecondSelectionCycleForTransientUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	err := types.NewErrorWithStatusCode(
		errors.New("Insufficient account balance"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)
	info := &relaycommon.RelayInfo{
		LastError:   err,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	require.True(t, shouldStartNextRelayChannelSelectionCycle(c, info, err, 0))
	require.False(t, shouldStartNextRelayChannelSelectionCycle(c, info, err, 1))
}

func TestRelaySecondSelectionCycleKeepsFailedChannelExcluded(t *testing.T) {
	retryParam := &service.RetryParam{}
	retryParam.ExcludeChannel(38)

	retryParam.ResetSelectionCycle()

	require.True(t, retryParam.ExcludedChannelIds[38])
}

func TestRelayExcludesFailedChannelBeforeSecondSelectionCycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	err := types.NewErrorWithStatusCode(
		errors.New("Service temporarily unavailable"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	)
	info := &relaycommon.RelayInfo{
		LastError:   err,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	retryParam := &service.RetryParam{}

	require.True(t, shouldStartNextRelayChannelSelectionCycle(c, info, err, 0))
	retryParam.ExcludeChannel(38)
	retryParam.ResetSelectionCycle()

	require.True(t, retryParam.ExcludedChannelIds[38])
}

func TestRelayDoesNotStartSecondSelectionCycleForClientErrorOrSpecificChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	clientErr := types.NewErrorWithStatusCode(
		errors.New("bad client request"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)

	require.False(t, shouldStartNextRelayChannelSelectionCycle(c, info, clientErr, 0))

	c.Set("specific_channel_id", 1)
	upstreamErr := types.NewErrorWithStatusCode(
		errors.New("upstream 500"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusInternalServerError,
	)
	require.False(t, shouldStartNextRelayChannelSelectionCycle(c, info, upstreamErr, 0))
}

func TestTaskRelayAffinitySkipKeepsChannelBeforeCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("channel_affinity_skip_retry_on_failure", true)
	err := &dto.TaskError{StatusCode: http.StatusForbidden}

	require.False(t, shouldRetryTaskRelay(c, 26, err, 1))
}

func TestSanitizeRelayErrorForUserHidesUpstreamMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("relay_channel_error_seen", true)
	err := types.NewOpenAIError(
		errors.New("upstream leaked provider detail"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusInternalServerError,
	)

	sanitized := sanitizeRelayErrorForUser(c, err)

	require.NotNil(t, sanitized)
	require.Equal(t, http.StatusInternalServerError, sanitized.StatusCode)
	require.Equal(t, "号池额度已耗尽正在切换号池，请重试", sanitized.Error())
	require.NotContains(t, sanitized.ToOpenAIError().Message, "provider detail")
}

func TestSanitizeRelayErrorForUserKeepsClientRequestError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	err := types.NewErrorWithStatusCode(
		errors.New("bad client request"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)

	require.Same(t, err, sanitizeRelayErrorForUser(c, err))
}

func TestSanitizeRelayErrorForUserKeepsProviderPolicyViolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("relay_channel_error_seen", true)
	err := types.NewOpenAIError(
		errors.New("Image request rejected by the provider content policy."),
		types.ErrorCodeContentPolicyViolation,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)

	sanitized := sanitizeRelayErrorForUser(c, err)

	require.Same(t, err, sanitized)
	require.Equal(t, http.StatusBadRequest, sanitized.StatusCode)
	require.Equal(t, types.ErrorCodeContentPolicyViolation, sanitized.GetErrorCode())
	require.False(t, service.IsTransientRelayFailoverError(sanitized))
}
