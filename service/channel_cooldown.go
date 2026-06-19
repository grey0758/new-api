package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	probeRequired bool
	probing       bool
	nextProbeAt   time.Time
	lastProbeAt   time.Time
	lastError     string
}

var (
	channelCooldownMu    sync.Mutex
	channelCooldownLocal = map[int]channelCooldownEntry{}
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
		recordChannelFailureForCooldownRedis(channelError, threshold, window, cooldown, modelName)
		return
	}
	recordChannelFailureForCooldownLocal(channelError, threshold, window, cooldown, modelName)
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
	if isTransientRelayFailoverMessage(strings.ToLower(err.Error())) {
		return true
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
	return code == 429 || code == 408 || code >= 500 || code == 401 || code == 403
}

func recordChannelFailureForCooldownRedis(channelError types.ChannelError, threshold int, window time.Duration, cooldown time.Duration, modelName string) {
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
	trackChannelCooldownProbe(channelError, modelName)
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）连续失败 %d 次，临时冷却 %s", channelError.ChannelName, channelError.ChannelId, count, cooldown.String()))
}

func recordChannelFailureForCooldownLocal(channelError types.ChannelError, threshold int, window time.Duration, cooldown time.Duration, modelName string) {
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
		entry.channelName = channelError.ChannelName
		entry.probeModel = strings.TrimSpace(modelName)
		entry.nextProbeAt = now
		entry.failures = 0
		entry.windowAt = now
		trackChannelCooldownProbeLocked(channelError, modelName)
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）连续失败 %d 次，临时冷却 %s", channelError.ChannelName, channelError.ChannelId, threshold, cooldown.String()))
	}
	channelCooldownLocal[channelError.ChannelId] = entry
}

func StartChannelCooldownProbeTask() {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		common.SysLog("channel cooldown active probe task started")
		for range ticker.C {
			runDueChannelCooldownProbes()
		}
	}()
}

func isChannelAwaitingProbe(channelId int) bool {
	channelCooldownMu.Lock()
	defer channelCooldownMu.Unlock()
	entry, ok := channelCooldownLocal[channelId]
	return ok && entry.probeRequired && !entry.coolUntil.IsZero()
}

func trackChannelCooldownProbe(channelError types.ChannelError, modelName string) {
	channelCooldownMu.Lock()
	defer channelCooldownMu.Unlock()
	trackChannelCooldownProbeLocked(channelError, modelName)
}

func trackChannelCooldownProbeLocked(channelError types.ChannelError, modelName string) {
	if !common.ChannelCooldownProbeEnabled || !supportsChannelCooldownProbe(channelError.ChannelType) {
		return
	}
	entry := channelCooldownLocal[channelError.ChannelId]
	entry.channelName = channelError.ChannelName
	entry.probeModel = strings.TrimSpace(modelName)
	entry.probeRequired = true
	if entry.coolUntil.IsZero() {
		cooldown := time.Duration(common.ChannelCooldownSeconds) * time.Second
		if cooldown <= 0 {
			cooldown = 5 * time.Minute
		}
		entry.coolUntil = time.Now().Add(cooldown)
	}
	entry.nextProbeAt = time.Now()
	channelCooldownLocal[channelError.ChannelId] = entry
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

func runDueChannelCooldownProbes() {
	if !common.AutomaticChannelCooldownEnabled || !common.ChannelCooldownProbeEnabled {
		return
	}
	interval := time.Duration(common.ChannelCooldownProbeIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	now := time.Now()
	type probeJob struct {
		channelId int
		modelName string
	}
	jobs := make([]probeJob, 0)
	channelCooldownMu.Lock()
	for channelId, entry := range channelCooldownLocal {
		if entry.coolUntil.IsZero() || !entry.probeRequired || entry.probing {
			continue
		}
		if !entry.nextProbeAt.IsZero() && now.Before(entry.nextProbeAt) {
			continue
		}
		entry.probing = true
		entry.lastProbeAt = now
		entry.nextProbeAt = now.Add(interval)
		channelCooldownLocal[channelId] = entry
		jobs = append(jobs, probeJob{channelId: channelId, modelName: entry.probeModel})
	}
	channelCooldownMu.Unlock()
	for _, job := range jobs {
		go probeAndRecoverChannelCooldown(job.channelId, job.modelName)
	}
}

func probeAndRecoverChannelCooldown(channelId int, modelName string) {
	timeout := time.Duration(common.ChannelCooldownProbeTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	err := probeChannelCooldown(channelId, modelName, timeout)
	if err == nil {
		ClearChannelCooldown(channelId)
		common.SysLog(fmt.Sprintf("通道 #%d 冷却主动探测通过，已恢复调度", channelId))
		return
	}
	channelCooldownMu.Lock()
	entry := channelCooldownLocal[channelId]
	entry.probing = false
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
	common.SysLog(fmt.Sprintf("通道 #%d 冷却主动探测未通过: %s", channelId, err.Error()))
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
		_ = common.RDB.Del(ctx, channelCooldownKey(channelId), channelCooldownFailureKey(channelId)).Err()
	}
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
		ClearChannelCooldown(channelId)
		return fmt.Errorf("channel is not enabled")
	}
	if !supportsChannelCooldownProbe(channel.Type) {
		ClearChannelCooldown(channelId)
		return fmt.Errorf("channel type %d does not support active cooldown probe", channel.Type)
	}
	modelName = resolveCooldownProbeModel(channel, modelName)
	if strings.TrimSpace(modelName) == "" {
		return fmt.Errorf("probe model is empty")
	}
	return probeOpenAICompatibleStream(channel, modelName, timeout)
}

func resolveCooldownProbeModel(channel *model.Channel, modelName string) string {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" && channel.TestModel != nil {
		modelName = strings.TrimSpace(*channel.TestModel)
	}
	if modelName == "" {
		models := channel.GetModels()
		if len(models) > 0 {
			modelName = strings.TrimSpace(models[0])
		}
	}
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
	if err := json.Unmarshal([]byte(modelMapping), &modelMap); err != nil {
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

func probeOpenAICompatibleStream(channel *model.Channel, modelName string, timeout time.Duration) error {
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
		"messages": []map[string]string{
			{"role": "user", "content": "你好"},
		},
		"stream":     true,
		"max_tokens": 16,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
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
	if err := json.Unmarshal(data, &payload); err != nil {
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

func channelCooldownKey(channelId int) string {
	return fmt.Sprintf("channel:cooldown:active:%d", channelId)
}
