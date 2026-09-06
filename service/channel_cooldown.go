package service

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
)

type channelCooldownEntry struct {
	failures      int
	windowAt      time.Time
	coolUntil     time.Time
	channelName   string
	probeModel    string
	actors        map[string]struct{}
	probeRequired bool
	probing       bool
	nextProbeAt   time.Time
	lastProbeAt   time.Time
	lastFailureAt time.Time
	lastError     string
}

type ChannelCooldownStatus struct {
	ChannelID              int    `json:"channel_id"`
	ChannelName            string `json:"channel_name,omitempty"`
	CoolingDown            bool   `json:"cooling_down"`
	CooldownTTLSeconds     int64  `json:"cooldown_ttl_seconds"`
	CoolUntil              int64  `json:"cool_until"`
	FailureCount           int    `json:"failure_count"`
	FailureWindowStartedAt int64  `json:"failure_window_started_at"`
	ActorCount             int64  `json:"actor_count"`
	ProbeRequired          bool   `json:"probe_required"`
	Probing                bool   `json:"probing"`
	ProbeModel             string `json:"probe_model,omitempty"`
	NextProbeAt            int64  `json:"next_probe_at"`
	LastProbeAt            int64  `json:"last_probe_at"`
	LastFailureAt          int64  `json:"last_failure_at"`
	LastError              string `json:"last_error,omitempty"`
	ActiveProbeEnabled     bool   `json:"active_probe_enabled"`
	ActiveProbeMode        string `json:"active_probe_mode,omitempty"`
}

type channelCooldownProbeEvent struct {
	eventType   string
	channelId   int
	channelName string
	modelName   string
	content     string
	other       map[string]interface{}
}

type channelCooldownProbeJob struct {
	channelId   int
	channelName string
	modelName   string
	mode        string
}

var (
	channelCooldownMu    sync.Mutex
	channelCooldownLocal = map[int]channelCooldownEntry{}
)

const channelCooldownProbeRecentErrorWindow = time.Hour
const channelCooldownProbeModel = "gpt-5.5"

const (
	ChannelCooldownProbeEndpoint = "/v1/responses"
	ChannelCooldownProbeProtocol = "openai-response"
)

const (
	channelProbeModeRecovery   = "cooldown_recovery"
	channelProbeModeContinuous = "continuous_active"
)

func IsChannelCoolingDown(channelId int) bool {
	if !common.AutomaticChannelCooldownEnabled || channelId <= 0 || common.ChannelCooldownSeconds <= 0 {
		return false
	}
	if common.ChannelCooldownProbeEnabled && isChannelAwaitingProbe(channelId) {
		return true
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
	RecordChannelFailureForCooldownWithModel(channelError, err, "")
}

func RecordChannelFailureForCooldownWithModel(channelError types.ChannelError, err *types.NewAPIError, modelName string) {
	RecordChannelFailureForCooldownWithActor(channelError, err, modelName, 0, 0)
}

func RecordChannelFailureForCooldownWithActor(channelError types.ChannelError, err *types.NewAPIError, modelName string, userId int, tokenId int) {
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
		recordChannelFailureForCooldownRedis(channelError, threshold, window, cooldown, modelName, channelCooldownActorKey(userId, tokenId))
		return
	}
	recordChannelFailureForCooldownLocal(channelError, threshold, window, cooldown, modelName, channelCooldownActorKey(userId, tokenId))
}

func shouldRecordChannelFailureForCooldown(channelError types.ChannelError, err *types.NewAPIError) bool {
	if !common.AutomaticChannelCooldownEnabled || channelError.ChannelId <= 0 || err == nil {
		return false
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	if IsResponsesStreamIncompleteError(err) {
		return false
	}
	if IsClientRequestValidationError(err) {
		return false
	}
	if IsRequestScopedUpstreamRejectionError(err) {
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
	if err.StatusCode == http.StatusTooManyRequests {
		return false
	}
	if isTransientRelayFailoverMessage(strings.ToLower(err.Error())) {
		return true
	}
	if serviceShouldCooldownByStatusCode(err.StatusCode) {
		return true
	}
	return ShouldDisableChannel(err)
}

func IsClientRequestValidationError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	lowerMessage := strings.ToLower(err.Error())
	lowerCode := strings.ToLower(string(err.GetErrorCode()))
	lowerType := strings.ToLower(string(err.GetErrorType()))
	if openAIErr, ok := err.RelayError.(types.OpenAIError); ok {
		lowerMessage += " " + strings.ToLower(openAIErr.Message)
		lowerType += " " + strings.ToLower(openAIErr.Type)
		lowerCode += " " + strings.ToLower(fmt.Sprintf("%v", openAIErr.Code))
	}
	if strings.Contains(lowerCode, "invalid_type") || strings.Contains(lowerType, "invalid_request") {
		return true
	}
	clientValidationCodes := []string{
		"unsupported_channel_endpoint",
		"duplicate_tool_output",
		"missing_tool_output",
		"invalid_tool_output",
		"outer_tool_session_not_found",
		"outer_tool_session_conflict",
		"prompt_cache_key_required",
	}
	for _, code := range clientValidationCodes {
		if strings.Contains(lowerCode, code) || strings.Contains(lowerMessage, code) {
			return true
		}
	}
	clientValidationHints := []string{
		"invalid type for",
		"expected an object",
		"expected object",
		"got a string instead",
		"unknown parameter",
		"missing required parameter",
		"invalid parameter",
		"invalid request body",
		"invalid request",
		"duplicate tool output",
		"missing tool output",
		"tool outputs must match",
		"paused outer-tool session",
		"no paused outer-tool session",
		"prompt_cache_key is required when tools",
		"call_id may have only one tool output",
	}
	for _, hint := range clientValidationHints {
		if strings.Contains(lowerMessage, hint) {
			return true
		}
	}
	return false
}

func IsRequestScopedUpstreamRejectionError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	lowerMessage := strings.ToLower(err.Error())
	lowerCode := strings.ToLower(string(err.GetErrorCode()))
	lowerType := strings.ToLower(string(err.GetErrorType()))
	if openAIErr, ok := err.RelayError.(types.OpenAIError); ok {
		lowerMessage += " " + strings.ToLower(openAIErr.Message)
		lowerType += " " + strings.ToLower(openAIErr.Type)
		lowerCode += " " + strings.ToLower(fmt.Sprintf("%v", openAIErr.Code))
	}
	requestScopedHints := []string{
		"cyber_policy",
		"cybersecurity risk",
		"content was flagged",
		"content policy violation",
		"content_policy_violation",
		"may violate our content policies",
		"violate our content policies",
		"no tool call found for function call output",
		"function call output with call_id",
	}
	for _, hint := range requestScopedHints {
		if strings.Contains(lowerMessage, hint) || strings.Contains(lowerCode, hint) || strings.Contains(lowerType, hint) {
			return true
		}
	}
	return false
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
	if strings.Contains(lowerMessage, "rate_limit_exceeded") {
		return true
	}
	if strings.Contains(lowerMessage, "concurrency limit exceeded") && strings.Contains(lowerMessage, "retry") {
		return true
	}
	if strings.Contains(lowerMessage, "billing service temporarily unavailable") && strings.Contains(lowerMessage, "retry") {
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
	if IsResponsesStreamIncompleteError(err) {
		return true
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

	if isTransientRelayFailoverMessage(lowerMessage) {
		return true
	}

	if err.StatusCode == http.StatusBadRequest || err.StatusCode == http.StatusForbidden || err.StatusCode == http.StatusTooManyRequests || err.StatusCode == http.StatusServiceUnavailable {
		if strings.Contains(lowerMessage, "rate limit") || strings.Contains(lowerMessage, "limit") || strings.Contains(lowerMessage, "quota") {
			return true
		}
	}

	return false
}

func isTransientRelayFailoverMessage(lowerMessage string) bool {
	if lowerMessage == "" {
		return false
	}
	transientHints := []string{
		"selected model is at capacity",
		"model is at capacity",
		"at capacity",
		"capacity_exceeded",
		"capacity exceeded",
		"over capacity",
		"model too slow",
		"too slow",
		"server_is_overloaded",
		"server overloaded",
		"servers are currently overloaded",
		"service temporarily unavailable",
		"temporarily unavailable",
		"upstream request failed",
		"upstream returned",
		"upstream error",
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
	return false
}

func isTransientUpstreamStatusCode(code int) bool {
	if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500 && code <= 599
}

func serviceShouldCooldownByStatusCode(code int) bool {
	return code == 408 || code >= 500 || code == 401 || code == 403
}

func recordChannelFailureForCooldownRedis(channelError types.ChannelError, threshold int, window time.Duration, cooldown time.Duration, modelName string, actorKey string) {
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
	if actorKey != "" {
		actorsKey := channelCooldownActorsKey(channelError.ChannelId)
		if err := common.RDB.SAdd(ctx, actorsKey, actorKey).Err(); err != nil {
			common.SysError(fmt.Sprintf("channel cooldown actor counter failed: channel #%d: %s", channelError.ChannelId, err.Error()))
			return
		}
		_ = common.RDB.Expire(ctx, actorsKey, window).Err()
	}
	if count < int64(threshold) {
		return
	}
	if actorKey != "" {
		actors, err := common.RDB.SCard(ctx, channelCooldownActorsKey(channelError.ChannelId)).Result()
		if err != nil {
			common.SysError(fmt.Sprintf("channel cooldown actor read failed: channel #%d: %s", channelError.ChannelId, err.Error()))
			return
		}
		if actors < 2 {
			return
		}
	}
	cooldownKey := channelCooldownKey(channelError.ChannelId)
	if err := common.RDB.Set(ctx, cooldownKey, "1", cooldown).Err(); err != nil {
		common.SysError(fmt.Sprintf("channel cooldown set failed: channel #%d: %s", channelError.ChannelId, err.Error()))
		return
	}
	_ = common.RDB.Del(ctx, failureKey, channelCooldownActorsKey(channelError.ChannelId)).Err()
	probeEnabled := trackChannelCooldownProbe(channelError, modelName)
	RecordChannelCooldownHealthEvent(ChannelHealthEventNewAPICooling, channelError.ChannelId, channelError.ChannelName, modelName, fmt.Sprintf("通道连续失败 %d 次，进入 NewAPI 渠道冷却", count), map[string]interface{}{
		"failure_count":    count,
		"cooldown_seconds": int64(cooldown.Seconds()),
	})
	if probeEnabled {
		RecordChannelCooldownHealthEvent(ChannelHealthEventProbeWaiting, channelError.ChannelId, channelError.ChannelName, channelCooldownProbeModel, "等待主动探针恢复", nil)
	}
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）连续失败 %d 次，临时冷却 %s", channelError.ChannelName, channelError.ChannelId, count, cooldown.String()))
}

func recordChannelFailureForCooldownLocal(channelError types.ChannelError, threshold int, window time.Duration, cooldown time.Duration, modelName string, actorKey string) {
	now := time.Now()
	var (
		probeEnabled     bool
		coolingTriggered bool
	)
	channelCooldownMu.Lock()
	entry := channelCooldownLocal[channelError.ChannelId]
	if entry.windowAt.IsZero() || now.Sub(entry.windowAt) > window {
		entry.windowAt = now
		entry.failures = 0
		entry.actors = nil
	}
	entry.failures++
	if actorKey != "" {
		if entry.actors == nil {
			entry.actors = map[string]struct{}{}
		}
		entry.actors[actorKey] = struct{}{}
	}
	if entry.failures >= threshold {
		if actorKey != "" && len(entry.actors) < 2 {
			channelCooldownLocal[channelError.ChannelId] = entry
			channelCooldownMu.Unlock()
			return
		}
		entry.coolUntil = now.Add(cooldown)
		entry.channelName = channelError.ChannelName
		entry.probeModel = strings.TrimSpace(modelName)
		entry.nextProbeAt = now
		entry.lastFailureAt = now
		entry.failures = 0
		entry.actors = nil
		entry.windowAt = now
		if shouldTrackChannelCooldownProbe(channelError) {
			entry = applyChannelCooldownProbeEntry(entry, channelError, now)
			probeEnabled = true
		}
		coolingTriggered = true
	}
	channelCooldownLocal[channelError.ChannelId] = entry
	channelCooldownMu.Unlock()
	if coolingTriggered {
		RecordChannelCooldownHealthEvent(ChannelHealthEventNewAPICooling, channelError.ChannelId, channelError.ChannelName, modelName, fmt.Sprintf("通道连续失败 %d 次，进入 NewAPI 渠道冷却", threshold), map[string]interface{}{
			"failure_count":    threshold,
			"cooldown_seconds": int64(cooldown.Seconds()),
		})
		if probeEnabled {
			RecordChannelCooldownHealthEvent(ChannelHealthEventProbeWaiting, channelError.ChannelId, channelError.ChannelName, channelCooldownProbeModel, "等待主动探针恢复", nil)
		}
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）连续失败 %d 次，临时冷却 %s", channelError.ChannelName, channelError.ChannelId, threshold, cooldown.String()))
	}
}

func channelCooldownActorKey(userId int, tokenId int) string {
	if userId <= 0 && tokenId <= 0 {
		return ""
	}
	return fmt.Sprintf("u:%d:t:%d", userId, tokenId)
}

func StartChannelCooldownProbeTask() {
	go func() {
		interval := time.Duration(common.ChannelCooldownProbeIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = time.Minute
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		common.SysLog("channel cooldown active probe task started")
		for range ticker.C {
			if model.IsBackgroundTaskLeader() {
				runDueChannelCooldownProbes()
			}
		}
	}()
}

func isChannelAwaitingProbe(channelId int) bool {
	channelCooldownMu.Lock()
	defer channelCooldownMu.Unlock()
	entry, ok := channelCooldownLocal[channelId]
	return ok && entry.probeRequired && !entry.coolUntil.IsZero()
}

func trackChannelCooldownProbe(channelError types.ChannelError, modelName string) bool {
	now := time.Now()
	if !shouldTrackChannelCooldownProbe(channelError) {
		return false
	}
	channelCooldownMu.Lock()
	defer channelCooldownMu.Unlock()
	return trackChannelCooldownProbeLocked(channelError, modelName, now)
}

func trackChannelCooldownProbeLocked(channelError types.ChannelError, modelName string, now time.Time) bool {
	entry := channelCooldownLocal[channelError.ChannelId]
	entry = applyChannelCooldownProbeEntry(entry, channelError, now)
	channelCooldownLocal[channelError.ChannelId] = entry
	return true
}

func applyChannelCooldownProbeEntry(entry channelCooldownEntry, channelError types.ChannelError, now time.Time) channelCooldownEntry {
	entry.channelName = channelError.ChannelName
	entry.probeModel = channelCooldownProbeModel
	entry.probeRequired = true
	entry.probing = false
	entry.lastFailureAt = now
	if entry.coolUntil.IsZero() {
		cooldown := time.Duration(common.ChannelCooldownSeconds) * time.Second
		if cooldown <= 0 {
			cooldown = 5 * time.Minute
		}
		entry.coolUntil = now.Add(cooldown)
	}
	entry.nextProbeAt = now
	return entry
}

func supportsChannelCooldownProbe(channelType int) bool {
	apiType, ok := common.ChannelType2APIType(channelType)
	if !ok {
		return false
	}
	switch apiType {
	case constant.APITypeOpenAI, constant.APITypeCLIProxy, constant.APITypeCodex, constant.APITypeOpenRouter:
		return true
	default:
		return false
	}
}

func shouldTrackChannelCooldownProbe(channelError types.ChannelError) bool {
	if !common.ChannelCooldownProbeEnabled || channelError.ChannelId <= 0 {
		return false
	}
	channel, err := model.CacheGetChannel(channelError.ChannelId)
	if err != nil || channel == nil {
		return false
	}
	if channel.Status != common.ChannelStatusEnabled {
		return false
	}
	if !supportsChannelCooldownProbe(channel.Type) {
		return false
	}
	enabled, _ := ChannelActiveProbeEligibility(channel)
	return enabled
}

func ChannelActiveProbeEligibility(channel *model.Channel) (bool, string) {
	if channel == nil {
		return false, ""
	}
	if channel.Status != common.ChannelStatusEnabled {
		return false, ""
	}
	if !supportsChannelCooldownProbe(channel.Type) {
		return false, ""
	}
	if channelHasManualActiveProbeEnabled(channel) {
		return true, "manual"
	}
	if channelNameHasRateSuffix(channel.Name) {
		return true, "rate_suffix"
	}
	return false, ""
}

func channelHasManualActiveProbeEnabled(channel *model.Channel) bool {
	if channel == nil {
		return false
	}
	other := channel.GetOtherInfo()
	for _, key := range []string{"active_probe_enabled", "channel_active_probe_enabled", "cooldown_active_probe_enabled"} {
		value, ok := other[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case bool:
			return v
		case string:
			return strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1"
		case float64:
			return v == 1
		case int:
			return v == 1
		}
	}
	return false
}

func channelNameHasRateSuffix(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	idx := strings.LastIndex(name, "-")
	if idx < 0 || idx == len(name)-1 {
		return false
	}
	suffix := strings.TrimSpace(name[idx+1:])
	if suffix == "" {
		return false
	}
	value, err := strconv.ParseFloat(suffix, 64)
	if err != nil || value <= 0 {
		return false
	}
	dotCount := 0
	for _, r := range suffix {
		if r == '.' {
			dotCount++
			if dotCount > 1 {
				return false
			}
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func runDueChannelCooldownProbes() {
	if !common.AutomaticChannelCooldownEnabled || !common.ChannelCooldownProbeEnabled {
		return
	}
	interval := time.Duration(common.ChannelCooldownProbeIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	now := time.Now()
	jobs := make([]channelCooldownProbeJob, 0)
	events := make([]channelCooldownProbeEvent, 0)
	channelCooldownMu.Lock()
	for channelId, entry := range channelCooldownLocal {
		if entry.coolUntil.IsZero() || !entry.probeRequired {
			continue
		}
		if entry.probing {
			continue
		}
		if !entry.nextProbeAt.IsZero() && now.Before(entry.nextProbeAt) {
			continue
		}
		modelName := strings.TrimSpace(entry.probeModel)
		if modelName == "" {
			modelName = channelCooldownProbeModel
			entry.probeModel = modelName
		}
		events = append(events, channelCooldownProbeEvent{
			eventType:   ChannelHealthEventProbeScanned,
			channelId:   channelId,
			channelName: entry.channelName,
			modelName:   modelName,
			content:     "主动探针扫描到待恢复渠道",
			other: map[string]interface{}{
				"next_probe_at":   unixOrZero(entry.nextProbeAt),
				"last_failure_at": unixOrZero(entry.lastFailureAt),
			},
		})
		if !entry.lastFailureAt.IsZero() && now.Sub(entry.lastFailureAt) > channelCooldownProbeRecentErrorWindow {
			entry.probeRequired = false
			channelCooldownLocal[channelId] = entry
			events = append(events, probeSkipEvent(channelId, entry.channelName, modelName, "recent_error_window_expired", "最近错误已超过主动探针窗口，停止恢复探针"))
			continue
		}
		channel, err := model.CacheGetChannel(channelId)
		if err != nil || channel == nil {
			entry.probeRequired = false
			channelCooldownLocal[channelId] = entry
			events = append(events, probeSkipEvent(channelId, entry.channelName, modelName, "channel_not_found", "渠道不存在，跳过主动探针"))
			continue
		}
		if entry.channelName == "" {
			entry.channelName = channel.Name
		}
		if channel.Status != common.ChannelStatusEnabled {
			entry.probeRequired = false
			channelCooldownLocal[channelId] = entry
			events = append(events, probeSkipEvent(channelId, entry.channelName, modelName, "channel_not_enabled", "渠道未启用，跳过主动探针"))
			continue
		}
		eligible, _ := ChannelActiveProbeEligibility(channel)
		if !eligible {
			entry.probeRequired = false
			channelCooldownLocal[channelId] = entry
			events = append(events, probeSkipEvent(channelId, entry.channelName, modelName, "not_in_active_probe_scope", "渠道不在主动探针范围，跳过恢复探针"))
			continue
		}
		if !supportsChannelCooldownProbe(channel.Type) {
			entry.probeRequired = false
			channelCooldownLocal[channelId] = entry
			events = append(events, probeSkipEvent(channelId, entry.channelName, modelName, "unsupported_channel_type", "渠道类型不支持主动探针，跳过"))
			continue
		}
		entry.probing = true
		entry.lastProbeAt = now
		entry.nextProbeAt = now.Add(interval)
		channelCooldownLocal[channelId] = entry
		jobs = append(jobs, channelCooldownProbeJob{channelId: channelId, channelName: entry.channelName, modelName: entry.probeModel, mode: channelProbeModeRecovery})
	}
	channelCooldownMu.Unlock()
	continuousJobs, continuousEvents := collectContinuousActiveProbeJobs(now, interval)
	jobs = append(jobs, continuousJobs...)
	events = append(events, continuousEvents...)
	for _, event := range events {
		RecordChannelCooldownHealthEvent(event.eventType, event.channelId, event.channelName, event.modelName, event.content, event.other)
	}
	for _, job := range jobs {
		content := "主动探针开始"
		if job.mode == channelProbeModeContinuous {
			content = "常规主动探针开始"
		}
		RecordChannelCooldownHealthEvent(ChannelHealthEventProbeStarted, job.channelId, job.channelName, job.modelName, content, map[string]interface{}{
			"probe_mode": job.mode,
		})
		go probeAndRecoverChannelCooldown(job.channelId, job.channelName, job.modelName, job.mode)
	}
}

func collectContinuousActiveProbeJobs(now time.Time, interval time.Duration) ([]channelCooldownProbeJob, []channelCooldownProbeEvent) {
	jobs := make([]channelCooldownProbeJob, 0)
	events := make([]channelCooldownProbeEvent, 0)
	channels := make([]*model.Channel, 0)
	if err := model.DB.Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		common.SysError("failed to load continuous active probe channels: " + err.Error())
		return nil, nil
	}
	for _, channel := range channels {
		eligible, mode := ChannelActiveProbeEligibility(channel)
		if !eligible {
			continue
		}
		channelCooldownMu.Lock()
		entry := channelCooldownLocal[channel.Id]
		if entry.probing || entry.probeRequired || (!entry.coolUntil.IsZero() && now.Before(entry.coolUntil)) {
			channelCooldownMu.Unlock()
			continue
		}
		if !entry.nextProbeAt.IsZero() && now.Before(entry.nextProbeAt) {
			channelCooldownMu.Unlock()
			continue
		}
		entry.channelName = channel.Name
		entry.probeModel = channelCooldownProbeModel
		entry.probing = true
		entry.lastProbeAt = now
		entry.nextProbeAt = now.Add(interval)
		channelCooldownLocal[channel.Id] = entry
		channelCooldownMu.Unlock()
		jobs = append(jobs, channelCooldownProbeJob{channelId: channel.Id, channelName: channel.Name, modelName: channelCooldownProbeModel, mode: channelProbeModeContinuous})
		events = append(events, channelCooldownProbeEvent{
			eventType:   ChannelHealthEventProbeScanned,
			channelId:   channel.Id,
			channelName: channel.Name,
			modelName:   channelCooldownProbeModel,
			content:     "主动探针扫描到持续探测渠道",
			other: map[string]interface{}{
				"active_probe_mode": mode,
				"probe_mode":        channelProbeModeContinuous,
				"next_probe_at":     now.Add(interval).Unix(),
			},
		})
	}
	return jobs, events
}

func probeSkipEvent(channelId int, channelName string, modelName string, reason string, content string) channelCooldownProbeEvent {
	return channelCooldownProbeEvent{
		eventType:   ChannelHealthEventProbeSkipped,
		channelId:   channelId,
		channelName: channelName,
		modelName:   modelName,
		content:     content,
		other: map[string]interface{}{
			"skip_reason": reason,
		},
	}
}

func probeAndRecoverChannelCooldown(channelId int, channelName string, modelName string, mode string) {
	timeout := time.Duration(common.ChannelCooldownProbeTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = time.Minute
	}
	err := probeChannelCooldown(channelId, modelName, timeout)
	if err == nil {
		recovered := markChannelProbeSucceeded(channelId, channelName)
		content := "常规主动探针通过"
		if recovered {
			content = "主动探针通过，渠道恢复调度"
		}
		RecordChannelCooldownHealthEvent(ChannelHealthEventProbeSucceeded, channelId, channelName, modelName, content, map[string]interface{}{
			"probe_mode": mode,
			"recovered":  recovered,
		})
		if recovered {
			common.SysLog(fmt.Sprintf("通道 #%d 冷却主动探测通过，已恢复调度", channelId))
		}
		return
	}
	if mode == channelProbeModeContinuous {
		markChannelCoolingFromActiveProbeFailure(channelId, channelName, modelName, err)
		RecordChannelCooldownHealthEvent(ChannelHealthEventNewAPICooling, channelId, channelName, modelName, "常规主动探针失败，进入 NewAPI 渠道冷却", map[string]interface{}{
			"probe_mode":       mode,
			"cooldown_seconds": common.ChannelCooldownSeconds,
		})
		RecordChannelCooldownHealthEvent(ChannelHealthEventProbeFailed, channelId, channelName, modelName, err.Error(), map[string]interface{}{
			"probe_mode": mode,
		})
		common.SysLog(fmt.Sprintf("通道 #%d 常规主动探测未通过，已进入冷却: %s", channelId, err.Error()))
		return
	}
	channelCooldownMu.Lock()
	entry := channelCooldownLocal[channelId]
	entry.probing = false
	entry.lastFailureAt = time.Now()
	entry.lastError = err.Error()
	if entry.nextProbeAt.IsZero() {
		interval := time.Duration(common.ChannelCooldownProbeIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = time.Minute
		}
		entry.nextProbeAt = time.Now().Add(interval)
	}
	channelCooldownLocal[channelId] = entry
	channelCooldownMu.Unlock()
	RecordChannelCooldownHealthEvent(ChannelHealthEventProbeFailed, channelId, entry.channelName, modelName, err.Error(), map[string]interface{}{
		"probe_mode": mode,
	})
	common.SysLog(fmt.Sprintf("通道 #%d 冷却主动探测未通过: %s", channelId, err.Error()))
}

func markChannelProbeSucceeded(channelId int, channelName string) bool {
	channelCooldownMu.Lock()
	defer channelCooldownMu.Unlock()
	entry, ok := channelCooldownLocal[channelId]
	if !ok {
		return false
	}
	recovered := entry.probeRequired || !entry.coolUntil.IsZero()
	if recovered {
		delete(channelCooldownLocal, channelId)
		return true
	}
	entry.channelName = channelName
	entry.probing = false
	entry.lastError = ""
	channelCooldownLocal[channelId] = entry
	return false
}

func markChannelCoolingFromActiveProbeFailure(channelId int, channelName string, modelName string, err error) {
	now := time.Now()
	cooldown := time.Duration(common.ChannelCooldownSeconds) * time.Second
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}
	interval := time.Duration(common.ChannelCooldownProbeIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	channelCooldownMu.Lock()
	entry := channelCooldownLocal[channelId]
	entry.channelName = channelName
	entry.probeModel = channelCooldownProbeModel
	if strings.TrimSpace(modelName) != "" {
		entry.probeModel = modelName
	}
	entry.coolUntil = now.Add(cooldown)
	entry.probeRequired = true
	entry.probing = false
	entry.lastFailureAt = now
	entry.lastError = err.Error()
	entry.nextProbeAt = now.Add(interval)
	channelCooldownLocal[channelId] = entry
	channelCooldownMu.Unlock()
}

func ClearChannelCooldown(channelId int) {
	if channelId <= 0 {
		return
	}
	channelCooldownMu.Lock()
	delete(channelCooldownLocal, channelId)
	channelCooldownMu.Unlock()
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		_ = common.RDB.Del(ctx, channelCooldownKey(channelId), channelCooldownFailureKey(channelId), channelCooldownActorsKey(channelId)).Err()
	}
}

func RecoverChannelCooldown(channelId int) ChannelCooldownStatus {
	statuses := GetChannelCooldownStatuses([]int{channelId})
	status := statuses[channelId]
	if status.ChannelID == 0 {
		status.ChannelID = channelId
	}
	ClearChannelCooldown(channelId)
	return status
}

func GetChannelCooldownStatuses(channelIds []int) map[int]ChannelCooldownStatus {
	statuses := make(map[int]ChannelCooldownStatus, len(channelIds))
	now := time.Now()

	channelCooldownMu.Lock()
	for _, channelId := range channelIds {
		entry, ok := channelCooldownLocal[channelId]
		if !ok {
			continue
		}
		status := ChannelCooldownStatus{
			ChannelID:              channelId,
			ChannelName:            entry.channelName,
			CoolingDown:            (!entry.coolUntil.IsZero() && now.Before(entry.coolUntil)) || entry.probeRequired || entry.probing,
			CooldownTTLSeconds:     secondsUntil(now, entry.coolUntil),
			CoolUntil:              unixOrZero(entry.coolUntil),
			FailureCount:           entry.failures,
			FailureWindowStartedAt: unixOrZero(entry.windowAt),
			ProbeRequired:          entry.probeRequired,
			Probing:                entry.probing,
			ProbeModel:             entry.probeModel,
			NextProbeAt:            unixOrZero(entry.nextProbeAt),
			LastProbeAt:            unixOrZero(entry.lastProbeAt),
			LastFailureAt:          unixOrZero(entry.lastFailureAt),
			LastError:              entry.lastError,
		}
		status.ActorCount = int64(len(entry.actors))
		statuses[channelId] = status
	}
	channelCooldownMu.Unlock()

	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		for _, channelId := range channelIds {
			status := statuses[channelId]
			if status.ChannelID == 0 {
				status.ChannelID = channelId
			}
			if ttl, err := common.RDB.TTL(ctx, channelCooldownKey(channelId)).Result(); err == nil && ttl > 0 {
				status.CoolingDown = true
				status.CooldownTTLSeconds = int64(ttl.Seconds())
				if status.CoolUntil == 0 {
					status.CoolUntil = now.Add(ttl).Unix()
				}
			}
			if count, err := common.RDB.Get(ctx, channelCooldownFailureKey(channelId)).Int(); err == nil && count > status.FailureCount {
				status.FailureCount = count
			}
			if actors, err := common.RDB.SCard(ctx, channelCooldownActorsKey(channelId)).Result(); err == nil && actors > status.ActorCount {
				status.ActorCount = actors
			}
			if status.ChannelID != 0 {
				statuses[channelId] = status
			}
		}
	}

	return statuses
}

func secondsUntil(now time.Time, t time.Time) int64 {
	if t.IsZero() || !now.Before(t) {
		return 0
	}
	return int64(t.Sub(now).Seconds())
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func probeChannelCooldown(channelId int, modelName string, timeout time.Duration) error {
	channel, err := model.CacheGetChannel(channelId)
	if err != nil {
		return fmt.Errorf("load channel failed: %w", err)
	}
	if channel == nil {
		return fmt.Errorf("channel not found")
	}
	if channel.Status != common.ChannelStatusEnabled {
		return fmt.Errorf("channel is not enabled")
	}
	if !supportsChannelCooldownProbe(channel.Type) {
		return fmt.Errorf("channel type %d does not support active cooldown probe", channel.Type)
	}
	modelName = resolveCooldownProbeModel(channel, modelName)
	if strings.TrimSpace(modelName) == "" {
		return fmt.Errorf("probe model is empty")
	}
	return probeOpenAIResponsesStream(channel, modelName, timeout)
}

func resolveCooldownProbeModel(channel *model.Channel, modelName string) string {
	modelName = channelCooldownProbeModel
	mapped, err := resolveCooldownProbeMappedModel(modelName, channel.GetModelMapping())
	if err == nil && mapped != "" {
		return mapped
	}
	return modelName
}

func resolveCooldownProbeMappedModel(modelName string, modelMapping string) (string, error) {
	if strings.TrimSpace(modelMapping) == "" || strings.TrimSpace(modelMapping) == "{}" {
		return modelName, nil
	}
	modelMap := map[string]string{}
	if err := common.Unmarshal([]byte(modelMapping), &modelMap); err != nil {
		return "", err
	}
	visited := map[string]bool{modelName: true}
	current := modelName
	for {
		next := strings.TrimSpace(modelMap[current])
		if next == "" {
			return current, nil
		}
		if visited[next] {
			return current, nil
		}
		visited[next] = true
		current = next
	}
}

func probeOpenAIResponsesStream(channel *model.Channel, modelName string, timeout time.Duration) error {
	key, _, keyErr := channel.GetNextEnabledKey()
	if keyErr != nil {
		return keyErr
	}
	baseURL := strings.TrimRight(channel.GetBaseURL(), "/")
	if baseURL == "" {
		return fmt.Errorf("base url is empty")
	}
	payload := map[string]interface{}{
		"model": modelName,
		"input": []map[string]string{
			{"role": "user", "content": "你好"},
		},
		"stream":            true,
		"max_output_tokens": 16,
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+ChannelCooldownProbeEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	for name, value := range channel.GetHeaderOverride() {
		if s, ok := value.(string); ok && strings.TrimSpace(name) != "" {
			req.Header.Set(name, s)
		}
	}
	client := &http.Client{Timeout: timeout + 5*time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("probe status %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		if cooldownProbeChunkHasContent([]byte(data)) {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("no valid stream content within %s", timeout.String())
}

func cooldownProbeChunkHasContent(data []byte) bool {
	payload := map[string]interface{}{}
	if err := common.Unmarshal(data, &payload); err != nil {
		return false
	}
	return cooldownProbeValueHasContent(payload)
}

func cooldownProbeValueHasContent(value interface{}) bool {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, child := range v {
			lowerKey := strings.ToLower(key)
			if lowerKey == "content" || lowerKey == "reasoning_content" || lowerKey == "text" || lowerKey == "delta" || lowerKey == "output_text" {
				if s, ok := child.(string); ok && strings.TrimSpace(s) != "" {
					return true
				}
			}
			if cooldownProbeValueHasContent(child) {
				return true
			}
		}
	case []interface{}:
		for _, child := range v {
			if cooldownProbeValueHasContent(child) {
				return true
			}
		}
	}
	return false
}

func channelCooldownFailureKey(channelId int) string {
	return fmt.Sprintf("channel:cooldown:failures:%d", channelId)
}

func channelCooldownActorsKey(channelId int) string {
	return fmt.Sprintf("channel:cooldown:actors:%d", channelId)
}

func channelCooldownKey(channelId int) string {
	return fmt.Sprintf("channel:cooldown:active:%d", channelId)
}
