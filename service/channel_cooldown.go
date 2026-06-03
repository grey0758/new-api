package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

type channelCooldownEntry struct {
	failures  int
	windowAt  time.Time
	coolUntil time.Time
}

var (
	channelCooldownMu    sync.Mutex
	channelCooldownLocal = map[int]channelCooldownEntry{}
)

func IsChannelCoolingDown(channelId int) bool {
	if !common.AutomaticChannelCooldownEnabled || channelId <= 0 || common.ChannelCooldownSeconds <= 0 {
		return false
	}
	if common.RedisEnabled && common.RDB != nil {
		ttl, err := common.RDB.TTL(context.Background(), channelCooldownKey(channelId)).Result()
		return err == nil && ttl > 0
	}
	now := time.Now()
	channelCooldownMu.Lock()
	defer channelCooldownMu.Unlock()
	entry, ok := channelCooldownLocal[channelId]
	if !ok || entry.coolUntil.IsZero() {
		return false
	}
	if now.Before(entry.coolUntil) {
		return true
	}
	delete(channelCooldownLocal, channelId)
	return false
}

func RecordChannelFailureForCooldown(channelError types.ChannelError, err *types.NewAPIError) {
	if !shouldRecordChannelFailureForCooldown(channelError, err) {
		return
	}
	threshold := common.ChannelCooldownFailureThreshold
	if threshold <= 0 {
		threshold = 1
	}
	window := time.Duration(common.ChannelCooldownFailureWindowSeconds) * time.Second
	if window <= 0 {
		window = 2 * time.Minute
	}
	cooldown := time.Duration(common.ChannelCooldownSeconds) * time.Second
	if cooldown <= 0 {
		return
	}
	if common.RedisEnabled && common.RDB != nil {
		recordChannelFailureForCooldownRedis(channelError, threshold, window, cooldown)
		return
	}
	recordChannelFailureForCooldownLocal(channelError, threshold, window, cooldown)
}

func shouldRecordChannelFailureForCooldown(channelError types.ChannelError, err *types.NewAPIError) bool {
	if !common.AutomaticChannelCooldownEnabled || channelError.ChannelId <= 0 || err == nil {
		return false
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	if IsTransientCredentialCooldownError(err) {
		return false
	}
	if IsTransientAuthUnavailableError(err) {
		return false
	}
	if IsTransientProviderCooldownError(err) {
		return false
	}
	if serviceShouldCooldownByStatusCode(err.StatusCode) {
		return true
	}
	return ShouldDisableChannel(err)
}

func IsTransientCredentialCooldownError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if err.StatusCode != 429 {
		return false
	}
	lowerMessage := strings.ToLower(err.Error())
	if strings.Contains(lowerMessage, "all credentials for model") && strings.Contains(lowerMessage, "cooling down") {
		return true
	}
	if strings.Contains(lowerMessage, "model_cooldown") {
		return true
	}
	if strings.Contains(lowerMessage, "reset_time") || strings.Contains(lowerMessage, "reset_seconds") {
		return true
	}
	if openAIErr, ok := err.RelayError.(types.OpenAIError); ok {
		lowerType := strings.ToLower(openAIErr.Type)
		lowerCode := strings.ToLower(fmt.Sprintf("%v", openAIErr.Code))
		if strings.Contains(lowerType, "model_cooldown") || strings.Contains(lowerCode, "model_cooldown") {
			return true
		}
	}
	return false
}

func IsTransientAuthUnavailableError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	lowerMessage := strings.ToLower(err.Error())
	if strings.Contains(lowerMessage, "auth_unavailable") {
		return true
	}
	if strings.Contains(lowerMessage, "no auth available") {
		return true
	}
	if strings.Contains(lowerMessage, "no available auth") {
		return true
	}
	if strings.Contains(lowerMessage, "no credential available") {
		return true
	}
	if openAIErr, ok := err.RelayError.(types.OpenAIError); ok {
		lowerType := strings.ToLower(openAIErr.Type)
		lowerCode := strings.ToLower(fmt.Sprintf("%v", openAIErr.Code))
		if strings.Contains(lowerType, "auth_unavailable") || strings.Contains(lowerCode, "auth_unavailable") {
			return true
		}
	}
	return false
}

func IsTransientProviderCooldownError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	lowerMessage := strings.ToLower(err.Error())
	if strings.Contains(lowerMessage, "cooling down") && strings.Contains(lowerMessage, "credential") {
		return true
	}
	if strings.Contains(lowerMessage, "rate limit") && (strings.Contains(lowerMessage, "cooldown") || strings.Contains(lowerMessage, "retry")) {
		return true
	}
	if strings.Contains(lowerMessage, "too many requests") && (strings.Contains(lowerMessage, "cooldown") || strings.Contains(lowerMessage, "retry")) {
		return true
	}
	if strings.Contains(lowerMessage, "冷却") {
		if strings.Contains(lowerMessage, "秒") || strings.Contains(lowerMessage, "分钟") || strings.Contains(lowerMessage, "minute") || strings.Contains(lowerMessage, "second") {
			return true
		}
		if strings.Contains(lowerMessage, "一次") || strings.Contains(lowerMessage, "次") || strings.Contains(lowerMessage, "频率") || strings.Contains(lowerMessage, "限流") {
			return true
		}
	}
	return false
}

// IsTransientRelayFailoverError reports upstream failures that should keep trying
// the next channel instead of being treated as a terminal client-side 400.
func IsTransientRelayFailoverError(err *types.NewAPIError) bool {
	if err == nil || types.IsSkipRetryError(err) {
		return false
	}
	if IsTransientCredentialCooldownError(err) || IsTransientAuthUnavailableError(err) || IsTransientProviderCooldownError(err) {
		return true
	}
	if isTransientUpstreamStatusCode(err.StatusCode) {
		return true
	}

	lowerMessage := strings.ToLower(err.Error())
	if lowerMessage == "" {
		return false
	}

	transientHints := []string{
		"quota exhausted",
		"insufficient_quota",
		"insufficient account balance",
		"insufficient balance",
		"account balance",
		"auth_unavailable",
		"no auth available",
		"no available auth",
		"no credential available",
		"model_cooldown",
		"cooling down",
		"credential",
		"reset_time",
		"reset_seconds",
		"unsupported_country_region_territory",
		"country_region_territory",
		"号池额度已耗尽",
		"账户余额不足",
		"账号余额不足",
		"余额不足",
		"正在切换号池",
		"请重试",
	}

	for _, hint := range transientHints {
		if strings.Contains(lowerMessage, hint) {
			return true
		}
	}

	if err.StatusCode == http.StatusBadRequest || err.StatusCode == http.StatusForbidden || err.StatusCode == http.StatusTooManyRequests || err.StatusCode == http.StatusServiceUnavailable {
		if strings.Contains(lowerMessage, "rate limit") || strings.Contains(lowerMessage, "limit") || strings.Contains(lowerMessage, "quota") {
			return true
		}
	}

	return false
}

func isTransientUpstreamStatusCode(code int) bool {
	if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500 && code <= 599
}

func serviceShouldCooldownByStatusCode(code int) bool {
	return code == 429 || code == 408 || code >= 500 || code == 401 || code == 403
}

func recordChannelFailureForCooldownRedis(channelError types.ChannelError, threshold int, window time.Duration, cooldown time.Duration) {
	ctx := context.Background()
	failureKey := channelCooldownFailureKey(channelError.ChannelId)
	count, err := common.RDB.Incr(ctx, failureKey).Result()
	if err != nil {
		common.SysError(fmt.Sprintf("channel cooldown failure counter failed: channel #%d: %s", channelError.ChannelId, err.Error()))
		return
	}
	if count == 1 {
		_ = common.RDB.Expire(ctx, failureKey, window).Err()
	}
	if count < int64(threshold) {
		return
	}
	cooldownKey := channelCooldownKey(channelError.ChannelId)
	if err := common.RDB.Set(ctx, cooldownKey, "1", cooldown).Err(); err != nil {
		common.SysError(fmt.Sprintf("channel cooldown set failed: channel #%d: %s", channelError.ChannelId, err.Error()))
		return
	}
	_ = common.RDB.Del(ctx, failureKey).Err()
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）连续失败 %d 次，临时冷却 %s", channelError.ChannelName, channelError.ChannelId, count, cooldown.String()))
}

func recordChannelFailureForCooldownLocal(channelError types.ChannelError, threshold int, window time.Duration, cooldown time.Duration) {
	now := time.Now()
	channelCooldownMu.Lock()
	defer channelCooldownMu.Unlock()
	entry := channelCooldownLocal[channelError.ChannelId]
	if entry.windowAt.IsZero() || now.Sub(entry.windowAt) > window {
		entry.windowAt = now
		entry.failures = 0
	}
	entry.failures++
	if entry.failures >= threshold {
		entry.coolUntil = now.Add(cooldown)
		entry.failures = 0
		entry.windowAt = now
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）连续失败 %d 次，临时冷却 %s", channelError.ChannelName, channelError.ChannelId, threshold, cooldown.String()))
	}
	channelCooldownLocal[channelError.ChannelId] = entry
}

func channelCooldownFailureKey(channelId int) string {
	return fmt.Sprintf("channel:cooldown:failures:%d", channelId)
}

func channelCooldownKey(channelId int) string {
	return fmt.Sprintf("channel:cooldown:active:%d", channelId)
}
