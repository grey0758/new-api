package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRecordChannelFailureForCooldownLocal(t *testing.T) {
	originalEnabled := common.AutomaticChannelCooldownEnabled
	originalRedisEnabled := common.RedisEnabled
	originalThreshold := common.ChannelCooldownFailureThreshold
	originalWindow := common.ChannelCooldownFailureWindowSeconds
	originalCooldown := common.ChannelCooldownSeconds
	t.Cleanup(func() {
		common.AutomaticChannelCooldownEnabled = originalEnabled
		common.RedisEnabled = originalRedisEnabled
		common.ChannelCooldownFailureThreshold = originalThreshold
		common.ChannelCooldownFailureWindowSeconds = originalWindow
		common.ChannelCooldownSeconds = originalCooldown
		channelCooldownMu.Lock()
		channelCooldownLocal = map[int]channelCooldownEntry{}
		channelCooldownMu.Unlock()
	})

	common.AutomaticChannelCooldownEnabled = true
	common.RedisEnabled = false
	common.ChannelCooldownFailureThreshold = 2
	common.ChannelCooldownFailureWindowSeconds = 60
	common.ChannelCooldownSeconds = 60
	channelError := *types.NewChannelError(9, 1, "test-channel", false, "", true)
	upstreamErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)

	RecordChannelFailureForCooldown(channelError, upstreamErr)
	require.False(t, IsChannelCoolingDown(9))

	RecordChannelFailureForCooldown(channelError, upstreamErr)
	require.True(t, IsChannelCoolingDown(9))
}

func TestIsChannelCoolingDownLocalExpires(t *testing.T) {
	originalEnabled := common.AutomaticChannelCooldownEnabled
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		common.AutomaticChannelCooldownEnabled = originalEnabled
		common.RedisEnabled = originalRedisEnabled
		channelCooldownMu.Lock()
		channelCooldownLocal = map[int]channelCooldownEntry{}
		channelCooldownMu.Unlock()
	})

	common.AutomaticChannelCooldownEnabled = true
	common.RedisEnabled = false
	channelCooldownMu.Lock()
	channelCooldownLocal[11] = channelCooldownEntry{coolUntil: time.Now().Add(-time.Second)}
	channelCooldownMu.Unlock()

	require.False(t, IsChannelCoolingDown(11))
}

func TestTransientCredentialCooldownDoesNotTriggerChannelCooldown(t *testing.T) {
	originalEnabled := common.AutomaticChannelCooldownEnabled
	originalRedisEnabled := common.RedisEnabled
	originalThreshold := common.ChannelCooldownFailureThreshold
	originalWindow := common.ChannelCooldownFailureWindowSeconds
	originalCooldown := common.ChannelCooldownSeconds
	t.Cleanup(func() {
		common.AutomaticChannelCooldownEnabled = originalEnabled
		common.RedisEnabled = originalRedisEnabled
		common.ChannelCooldownFailureThreshold = originalThreshold
		common.ChannelCooldownFailureWindowSeconds = originalWindow
		common.ChannelCooldownSeconds = originalCooldown
		channelCooldownMu.Lock()
		channelCooldownLocal = map[int]channelCooldownEntry{}
		channelCooldownMu.Unlock()
	})

	common.AutomaticChannelCooldownEnabled = true
	common.RedisEnabled = false
	common.ChannelCooldownFailureThreshold = 1
	common.ChannelCooldownFailureWindowSeconds = 60
	common.ChannelCooldownSeconds = 60

	channelError := *types.NewChannelError(29, 1, "test-channel", false, "", true)
	upstreamErr := types.NewErrorWithStatusCode(
		errors.New("All credentials for model gpt-5.5 are cooling down via provider codex"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	)

	require.True(t, IsTransientCredentialCooldownError(upstreamErr))
	RecordChannelFailureForCooldown(channelError, upstreamErr)
	require.False(t, IsChannelCoolingDown(29))
}

func TestAuthUnavailableDoesNotTriggerChannelCooldown(t *testing.T) {
	originalEnabled := common.AutomaticChannelCooldownEnabled
	originalRedisEnabled := common.RedisEnabled
	originalThreshold := common.ChannelCooldownFailureThreshold
	originalWindow := common.ChannelCooldownFailureWindowSeconds
	originalCooldown := common.ChannelCooldownSeconds
	t.Cleanup(func() {
		common.AutomaticChannelCooldownEnabled = originalEnabled
		common.RedisEnabled = originalRedisEnabled
		common.ChannelCooldownFailureThreshold = originalThreshold
		common.ChannelCooldownFailureWindowSeconds = originalWindow
		common.ChannelCooldownSeconds = originalCooldown
		channelCooldownMu.Lock()
		channelCooldownLocal = map[int]channelCooldownEntry{}
		channelCooldownMu.Unlock()
	})

	common.AutomaticChannelCooldownEnabled = true
	common.RedisEnabled = false
	common.ChannelCooldownFailureThreshold = 1
	common.ChannelCooldownFailureWindowSeconds = 60
	common.ChannelCooldownSeconds = 60

	channelError := *types.NewChannelError(31, 1, "test-channel", false, "", true)
	upstreamErr := types.NewErrorWithStatusCode(
		errors.New("auth_unavailable: no auth available (providers=codex, model=gpt-5.5)"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	)

	require.True(t, IsTransientAuthUnavailableError(upstreamErr))
	RecordChannelFailureForCooldown(channelError, upstreamErr)
	require.False(t, IsChannelCoolingDown(31))
}

func TestTransientProviderCooldownDoesNotTriggerChannelCooldown(t *testing.T) {
	originalEnabled := common.AutomaticChannelCooldownEnabled
	originalRedisEnabled := common.RedisEnabled
	originalThreshold := common.ChannelCooldownFailureThreshold
	originalWindow := common.ChannelCooldownFailureWindowSeconds
	originalCooldown := common.ChannelCooldownSeconds
	t.Cleanup(func() {
		common.AutomaticChannelCooldownEnabled = originalEnabled
		common.RedisEnabled = originalRedisEnabled
		common.ChannelCooldownFailureThreshold = originalThreshold
		common.ChannelCooldownFailureWindowSeconds = originalWindow
		common.ChannelCooldownSeconds = originalCooldown
		channelCooldownMu.Lock()
		channelCooldownLocal = map[int]channelCooldownEntry{}
		channelCooldownMu.Unlock()
	})

	common.AutomaticChannelCooldownEnabled = true
	common.RedisEnabled = false
	common.ChannelCooldownFailureThreshold = 1
	common.ChannelCooldownFailureWindowSeconds = 60
	common.ChannelCooldownSeconds = 60

	channelError := *types.NewChannelError(30, 1, "test-channel", false, "", true)
	upstreamErr := types.NewErrorWithStatusCode(
		errors.New("400: 一分钟30次。冷却20秒"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)

	require.True(t, IsTransientProviderCooldownError(upstreamErr))
	RecordChannelFailureForCooldown(channelError, upstreamErr)
	require.False(t, IsChannelCoolingDown(30))
}

func TestTransientRelayFailoverErrorIncludesQuotaAndCountryRegionHints(t *testing.T) {
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("400 Bad Request: 号池额度已耗尽正在切换号池，请重试"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)))
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("403 unsupported_country_region_territory"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)))
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("Insufficient account balance"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)))
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("账号余额不足"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusForbidden,
	)))
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("bad response status code 502"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)))
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("bad response status code 504"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusGatewayTimeout,
	)))
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("responses stream closed before response.completed"),
		types.ErrorCodeResponsesStreamIncomplete,
		http.StatusServiceUnavailable,
	)))
	require.False(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("invalid request body"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)))
}
