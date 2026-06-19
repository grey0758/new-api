package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	securityPromptExcerptKey = "security_prompt_excerpt"
	securityPromptHashKey    = "security_prompt_sha256"
	securityPromptLengthKey  = "security_prompt_length"
	securityFullPromptKey    = "security_full_prompt_context"
	securityRawRequestKey    = "security_raw_request_body"
	securityAuditEventsKey   = "security_audit_events"
)

const maxSecurityPromptExcerptRunes = 2000

var defaultPromptViolationTerms = []string{
	"jailbreak",
	"prompt injection",
	"dan mode",
	"do anything now",
	"developer mode",
	"developer mode enabled",
	"ignore previous instructions",
	"ignore all previous instructions",
	"ignore your instructions",
	"ignore your safety",
	"bypass safety",
	"bypass content policy",
	"bypass openai",
	"bypass claude",
	"unrestricted mode",
	"unfiltered response",
	"no ethical constraints",
	"no moral constraints",
	"do not refuse",
	"never refuse",
	"output the forbidden",
	"claude jailbreak",
	"gpt jailbreak",
	"gpt-5.5 jailbreak",
	"破解限制",
	"绕过限制",
	"绕过安全",
	"绕过审查",
	"解除限制",
	"越狱",
	"提示词注入",
	"无视以上",
	"忽略以上",
	"忽略之前",
	"忽略所有之前",
	"忽略安全策略",
	"开发者模式",
	"dan模式",
	"不受限制",
	"无审查",
	"不要拒绝",
	"不允许拒绝",
	"扮演dan",
	"输出违规",
	"gpt破解",
	"claude破解",
}

func PromptViolationCaptureEnabled() bool {
	return true
}

func RecordPromptViolationIfDetected(c *gin.Context, modelName string, promptText string) {
	storeSecurityPromptEvidence(c, promptText)
	matched := detectDefaultPromptViolationTerms(promptText)
	if len(matched) == 0 {
		return
	}
	rememberSecurityAuditEvent(c, modelName, map[string]interface{}{
		"security_event": "prompt_violation_detected",
		"action":         "record_only",
		"matched_terms":  matched,
	})
}

func RecordLocalSensitiveWordsIfDetected(c *gin.Context, modelName string, words []string) {
	if len(words) == 0 {
		return
	}
	matched := append([]string(nil), words...)
	sort.Strings(matched)
	rememberSecurityAuditEvent(c, modelName, map[string]interface{}{
		"security_event": "local_sensitive_words_detected",
		"action":         "record_only",
		"matched_terms":  matched,
	})
}

func RecordUpstream400ViolationCandidate(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	if c == nil || err == nil || err.StatusCode != http.StatusBadRequest {
		return
	}
	modelName := c.GetString("original_model")
	other := map[string]interface{}{
		"security_event": "upstream_400_violation_candidate",
		"action":         "record_only",
		"status_code":    err.StatusCode,
		"error_type":     err.GetErrorType(),
		"error_code":     err.GetErrorCode(),
		"channel_id":     channelError.ChannelId,
		"channel_name":   channelError.ChannelName,
		"channel_type":   channelError.ChannelType,
	}
	recordSecurityViolationLog(c, channelError.ChannelId, modelName, "upstream_400_violation_candidate", other)
}

func rememberSecurityAuditEvent(c *gin.Context, modelName string, event map[string]interface{}) {
	if c == nil || len(event) == 0 {
		return
	}
	if modelName == "" {
		modelName = c.GetString("original_model")
	}
	eventCopy := make(map[string]interface{}, len(event)+5)
	for key, value := range event {
		eventCopy[key] = value
	}
	if modelName != "" {
		eventCopy["model_name"] = modelName
	}
	if c.Request != nil && c.Request.URL != nil {
		eventCopy["request_path"] = c.Request.URL.Path
	}
	eventCopy["prompt_sha256"] = c.GetString(securityPromptHashKey)
	eventCopy["prompt_length"] = c.GetInt(securityPromptLengthKey)

	events := getSecurityAuditEvents(c)
	events = append(events, eventCopy)
	c.Set(securityAuditEventsKey, events)
}

func getSecurityAuditEvents(c *gin.Context) []map[string]interface{} {
	if c == nil {
		return nil
	}
	value, exists := c.Get(securityAuditEventsKey)
	if !exists {
		return nil
	}
	events, _ := value.([]map[string]interface{})
	return events
}

func AppendSecurityAuditToOther(c *gin.Context, other map[string]interface{}) map[string]interface{} {
	events := getSecurityAuditEvents(c)
	if len(events) == 0 {
		return other
	}
	if other == nil {
		other = make(map[string]interface{})
	}
	safeEvents := make([]map[string]interface{}, 0, len(events))
	for _, event := range events {
		eventCopy := make(map[string]interface{}, len(event))
		for key, value := range event {
			eventCopy[key] = value
		}
		safeEvents = append(safeEvents, eventCopy)
	}
	other["security_events"] = safeEvents
	other["security_event_count"] = len(safeEvents)
	return other
}

func detectDefaultPromptViolationTerms(text string) []string {
	if text == "" {
		return nil
	}
	lowerText := strings.ToLower(text)
	seen := make(map[string]struct{})
	for _, term := range defaultPromptViolationTerms {
		normalizedTerm := strings.ToLower(strings.TrimSpace(term))
		if normalizedTerm == "" {
			continue
		}
		if strings.Contains(lowerText, normalizedTerm) {
			seen[term] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	matched := make([]string, 0, len(seen))
	for term := range seen {
		matched = append(matched, term)
	}
	sort.Strings(matched)
	return matched
}

func storeSecurityPromptEvidence(c *gin.Context, promptText string) {
	if c == nil {
		return
	}
	c.Set(securityPromptLengthKey, utf8.RuneCountInString(promptText))
	c.Set(securityPromptHashKey, sha256Hex(promptText))
	c.Set(securityPromptExcerptKey, buildPromptExcerpt(promptText))
	c.Set(securityFullPromptKey, promptText)
	if rawBody, err := readRawRequestBodyForEvidence(c); err == nil {
		c.Set(securityRawRequestKey, rawBody)
	}
}

func recordSecurityViolationLog(c *gin.Context, channelId int, modelName string, content string, other map[string]interface{}) {
	if c == nil {
		return
	}
	userId := c.GetInt("id")
	tokenName := c.GetString("token_name")
	tokenId := c.GetInt("token_id")
	userGroup := c.GetString("group")
	if modelName == "" {
		modelName = c.GetString("original_model")
	}
	if other == nil {
		other = make(map[string]interface{})
	}
	if c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	other["prompt_sha256"] = c.GetString(securityPromptHashKey)
	other["prompt_length"] = c.GetInt(securityPromptLengthKey)
	other["prompt_excerpt"] = c.GetString(securityPromptExcerptKey)
	other["full_prompt_context"] = c.GetString(securityFullPromptKey)
	other["raw_request_body"] = c.GetString(securityRawRequestKey)
	other["admin_info"] = map[string]interface{}{
		"use_channel": c.GetStringSlice("use_channel"),
	}
	model.RecordErrorLog(c, userId, channelId, modelName, tokenName, content, tokenId, 0, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
}

func readRawRequestBodyForEvidence(c *gin.Context) (string, error) {
	if c == nil {
		return "", nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return "", err
	}
	bodyBytes, err := storage.Bytes()
	if err != nil {
		return "", fmt.Errorf("read raw request body for security evidence: %w", err)
	}
	return strings.ReplaceAll(string(bodyBytes), "\x00", ""), nil
}

func buildPromptExcerpt(text string) string {
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > maxSecurityPromptExcerptRunes {
		runes = runes[:maxSecurityPromptExcerptRunes]
	}
	excerpt := string(runes)
	excerpt = common.MaskSensitiveInfo(excerpt)
	excerpt = strings.ReplaceAll(excerpt, "\x00", "")
	return excerpt
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
