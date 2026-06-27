package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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

func TestSingleActorFailuresDoNotTriggerChannelCooldown(t *testing.T) {
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
	channelError := *types.NewChannelError(33, 1, "shared-channel", false, "", true)
	upstreamErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)

	RecordChannelFailureForCooldownWithActor(channelError, upstreamErr, "gpt-5.5", 161, 211)
	RecordChannelFailureForCooldownWithActor(channelError, upstreamErr, "gpt-5.5", 161, 211)

	require.False(t, IsChannelCoolingDown(33))
}

func TestMultipleActorFailuresTriggerChannelCooldown(t *testing.T) {
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
	channelError := *types.NewChannelError(34, 1, "shared-channel", false, "", true)
	upstreamErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)

	RecordChannelFailureForCooldownWithActor(channelError, upstreamErr, "gpt-5.5", 161, 211)
	RecordChannelFailureForCooldownWithActor(channelError, upstreamErr, "gpt-5.5", 266, 340)

	require.True(t, IsChannelCoolingDown(34))
}

func TestClientRequestValidationErrorDoesNotTriggerChannelCooldown(t *testing.T) {
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
	channelError := *types.NewChannelError(35, 1, "shared-channel", false, "", true)
	upstreamErr := types.NewOpenAIError(
		errors.New("Invalid type for 'input[7].arguments': expected an object, but got a string instead."),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
	)

	require.True(t, IsClientRequestValidationError(upstreamErr))
	RecordChannelFailureForCooldownWithActor(channelError, upstreamErr, "gpt-5.5", 161, 211)
	require.False(t, IsChannelCoolingDown(35))
}

func TestResponsesStreamIncompleteDoesNotTriggerChannelCooldown(t *testing.T) {
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
	channelError := *types.NewChannelError(36, 1, "sse-channel", false, "", true)
	upstreamErr := types.NewOpenAIError(
		errors.New("responses stream closed before response.completed: end=reason=timeout received=0 sent=0"),
		types.ErrorCodeResponsesStreamIncomplete,
		http.StatusServiceUnavailable,
	)

	require.True(t, IsResponsesStreamIncompleteError(upstreamErr))
	RecordChannelFailureForCooldownWithActor(channelError, upstreamErr, "gpt-5.5", 161, 211)
	require.False(t, IsChannelCoolingDown(36))
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

func TestClearChannelCooldownRemovesProbeRequiredEntry(t *testing.T) {
	originalEnabled := common.AutomaticChannelCooldownEnabled
	originalProbeEnabled := common.ChannelCooldownProbeEnabled
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		common.AutomaticChannelCooldownEnabled = originalEnabled
		common.ChannelCooldownProbeEnabled = originalProbeEnabled
		common.RedisEnabled = originalRedisEnabled
		channelCooldownMu.Lock()
		channelCooldownLocal = map[int]channelCooldownEntry{}
		channelCooldownMu.Unlock()
	})

	common.AutomaticChannelCooldownEnabled = true
	common.ChannelCooldownProbeEnabled = true
	common.RedisEnabled = false
	channelCooldownMu.Lock()
	channelCooldownLocal[12] = channelCooldownEntry{
		coolUntil:     time.Now().Add(time.Hour),
		probeRequired: true,
		nextProbeAt:   time.Now(),
	}
	channelCooldownMu.Unlock()

	require.True(t, IsChannelCoolingDown(12))
	ClearChannelCooldown(12)
	require.False(t, IsChannelCoolingDown(12))
}

func TestRecoverChannelCooldownReturnsPreviousStateAndClears(t *testing.T) {
	originalEnabled := common.AutomaticChannelCooldownEnabled
	originalProbeEnabled := common.ChannelCooldownProbeEnabled
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		common.AutomaticChannelCooldownEnabled = originalEnabled
		common.ChannelCooldownProbeEnabled = originalProbeEnabled
		common.RedisEnabled = originalRedisEnabled
		channelCooldownMu.Lock()
		channelCooldownLocal = map[int]channelCooldownEntry{}
		channelCooldownMu.Unlock()
	})

	common.AutomaticChannelCooldownEnabled = true
	common.ChannelCooldownProbeEnabled = true
	common.RedisEnabled = false
	channelCooldownMu.Lock()
	channelCooldownLocal[14] = channelCooldownEntry{
		channelName:   "cooling-channel",
		coolUntil:     time.Now().Add(time.Minute),
		probeRequired: true,
		probeModel:    channelCooldownProbeModel,
		nextProbeAt:   time.Now(),
		lastError:     "probe timeout",
	}
	channelCooldownMu.Unlock()

	status := RecoverChannelCooldown(14)

	require.Equal(t, 14, status.ChannelID)
	require.True(t, status.CoolingDown)
	require.True(t, status.ProbeRequired)
	require.Equal(t, "cooling-channel", status.ChannelName)
	require.False(t, IsChannelCoolingDown(14))
}

func TestApplyChannelCooldownProbeEntryKeepsProbeRequired(t *testing.T) {
	now := time.Now()
	entry := channelCooldownEntry{
		coolUntil: now.Add(5 * time.Minute),
	}
	channelError := *types.NewChannelError(21, 1, "probe-channel", false, "", true)

	entry = applyChannelCooldownProbeEntry(entry, channelError, now)

	require.True(t, entry.probeRequired)
	require.False(t, entry.probing)
	require.Equal(t, "probe-channel", entry.channelName)
	require.Equal(t, channelCooldownProbeModel, entry.probeModel)
	require.Equal(t, now, entry.nextProbeAt)
	require.Equal(t, now, entry.lastFailureAt)
}

func TestChannelCooldownStatusStaysCoolingWhileProbeRequiredAfterTTL(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		channelCooldownMu.Lock()
		channelCooldownLocal = map[int]channelCooldownEntry{}
		channelCooldownMu.Unlock()
	})

	common.RedisEnabled = false
	channelCooldownMu.Lock()
	channelCooldownLocal[22] = channelCooldownEntry{
		coolUntil:     time.Now().Add(-time.Minute),
		probeRequired: true,
		probeModel:    channelCooldownProbeModel,
		nextProbeAt:   time.Now(),
		lastFailureAt: time.Now().Add(-2 * time.Minute),
		channelName:   "ttl-expired-probe-channel",
	}
	channelCooldownMu.Unlock()

	statuses := GetChannelCooldownStatuses([]int{22})
	status := statuses[22]
	require.True(t, status.CoolingDown)
	require.True(t, status.ProbeRequired)
	require.Equal(t, channelCooldownProbeModel, status.ProbeModel)
}

func TestCooldownProbeChunkHasContent(t *testing.T) {
	require.False(t, cooldownProbeChunkHasContent([]byte(`{"choices":[{"delta":{"role":"assistant"}}]}`)))
	require.True(t, cooldownProbeChunkHasContent([]byte(`{"choices":[{"delta":{"content":"你好"}}]}`)))
	require.True(t, cooldownProbeChunkHasContent([]byte(`{"type":"response.output_text.delta","delta":"你"}`)))
	require.False(t, cooldownProbeChunkHasContent([]byte(`{"error":{"message":"bad"}}`)))
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

func TestTooManyRequestsDoesNotTriggerChannelCooldown(t *testing.T) {
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

	channelError := *types.NewChannelError(33, 1, "rate-limited-channel", false, "", true)
	upstreamErr := types.NewErrorWithStatusCode(
		errors.New("bad response status code 429"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	)

	require.True(t, IsTransientRelayFailoverError(upstreamErr))
	RecordChannelFailureForCooldown(channelError, upstreamErr)
	require.False(t, IsChannelCoolingDown(33))
	require.False(t, serviceShouldCooldownByStatusCode(http.StatusTooManyRequests))
	require.True(t, serviceShouldCooldownByStatusCode(http.StatusBadGateway))
}

func TestRequestScopedUpstreamRejectionDoesNotTriggerChannelCooldown(t *testing.T) {
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

	cases := []struct {
		name string
		err  *types.NewAPIError
	}{
		{
			name: "cyber policy",
			err: types.NewErrorWithStatusCode(
				errors.New("responses stream error: type=response.failed code=cyber_policy message=This content was flagged for possible cybersecurity risk."),
				types.ErrorCodeBadResponseStatusCode,
				http.StatusServiceUnavailable,
			),
		},
		{
			name: "tool call output state",
			err: types.NewErrorWithStatusCode(
				errors.New("Request failed with status 400: No tool call found for function call output with call_id fc_123."),
				types.ErrorCodeBadResponseStatusCode,
				http.StatusServiceUnavailable,
			),
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channelID := 40 + i
			channelError := *types.NewChannelError(channelID, 1, "request-scoped", false, "", true)
			require.True(t, IsTransientRelayFailoverError(tc.err))
			require.True(t, IsRequestScopedUpstreamRejectionError(tc.err))
			RecordChannelFailureForCooldown(channelError, tc.err)
			require.False(t, IsChannelCoolingDown(channelID))
		})
	}
}

func TestProviderTransientLimitErrorsDoNotTriggerChannelCooldown(t *testing.T) {
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

	cases := []struct {
		name string
		err  *types.NewAPIError
	}{
		{
			name: "account concurrency limit",
			err: types.NewErrorWithStatusCode(
				errors.New("responses stream error: type=response.failed code=rate_limit_exceeded message=Concurrency limit exceeded for account, please retry later"),
				types.ErrorCodeBadResponseStatusCode,
				http.StatusServiceUnavailable,
			),
		},
		{
			name: "billing temporarily unavailable",
			err: types.NewErrorWithStatusCode(
				errors.New("Billing service temporarily unavailable. Please retry later."),
				types.ErrorCodeBadResponseStatusCode,
				http.StatusServiceUnavailable,
			),
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channelID := 50 + i
			channelError := *types.NewChannelError(channelID, 1, "provider-transient", false, "", true)
			require.True(t, IsTransientRelayFailoverError(tc.err))
			require.True(t, IsTransientProviderCooldownError(tc.err))
			RecordChannelFailureForCooldown(channelError, tc.err)
			require.False(t, IsChannelCoolingDown(channelID))
		})
	}
}

func TestTransientRelayFailoverErrorIncludesQuotaAndCountryRegionHints(t *testing.T) {
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("Selected model is at capacity. Please try a different model."),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)))
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
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("model too slow, please try again later"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)))
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("Upstream request failed"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)))
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("upstream returned error"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)))
	require.True(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("Service temporarily unavailable"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)))
	require.False(t, IsTransientRelayFailoverError(types.NewErrorWithStatusCode(
		errors.New("invalid request body"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)))
}

func TestTransientSlowUpstreamBadRequestTriggersChannelCooldown(t *testing.T) {
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

	channelError := *types.NewChannelError(32, 1, "slow-upstream", false, "", true)
	upstreamErr := types.NewErrorWithStatusCode(
		errors.New("model too slow, please try again later"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)

	RecordChannelFailureForCooldown(channelError, upstreamErr)
	require.True(t, IsChannelCoolingDown(32))
}

func TestChannelSupportsActiveProbeGroup(t *testing.T) {
	require.True(t, channelSupportsActiveProbeGroup(&model.Channel{Group: "default, plus"}))
	require.True(t, channelSupportsActiveProbeGroup(&model.Channel{Group: "pro"}))
	require.True(t, channelSupportsActiveProbeGroup(&model.Channel{Group: "PRO"}))
	require.False(t, channelSupportsActiveProbeGroup(&model.Channel{Group: "default"}))
	require.False(t, channelSupportsActiveProbeGroup(nil))
}

func TestResolveCooldownProbeModelAlwaysUsesGPT55(t *testing.T) {
	testModel := "gpt-5.4-mini"
	modelMapping := `{"gpt-5.5":"upstream-gpt-5.5"}`
	channel := &model.Channel{
		TestModel:    &testModel,
		Models:       "gpt-5.4-mini,gpt-5.5",
		ModelMapping: &modelMapping,
	}

	require.Equal(t, "upstream-gpt-5.5", resolveCooldownProbeModel(channel, "gpt-5.4-mini"))
}
