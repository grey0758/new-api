package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type channelHealthLogStat struct {
	ChannelId     int     `json:"channel_id" gorm:"column:channel_id"`
	TotalRequests int64   `json:"total_requests" gorm:"column:total_requests"`
	ErrorRequests int64   `json:"error_requests" gorm:"column:error_requests"`
	AvgUseTime    float64 `json:"avg_use_time" gorm:"column:avg_use_time"`
	LastSeenAt    int64   `json:"last_seen_at" gorm:"column:last_seen_at"`
}

type channelHealthEventStat struct {
	ChannelID               int    `json:"channel_id"`
	FinalErrors             int64  `json:"final_errors"`
	FailoverErrors          int64  `json:"failover_errors"`
	ProviderCooldowns       int64  `json:"provider_cooldowns"`
	NewAPICooldowns         int64  `json:"newapi_cooldowns"`
	ProbeWaiting            int64  `json:"probe_waiting"`
	ProbeScanned            int64  `json:"probe_scanned"`
	ProbeSkipped            int64  `json:"probe_skipped"`
	ProbeStarted            int64  `json:"probe_started"`
	ProbeFailed             int64  `json:"probe_failed"`
	ProbeSucceeded          int64  `json:"probe_succeeded"`
	ManualRecovered         int64  `json:"manual_recovered"`
	LastEventAt             int64  `json:"last_event_at"`
	LastEventType           string `json:"last_event_type"`
	LastEventMessage        string `json:"last_event_message"`
	LastStatusCode          int    `json:"last_status_code"`
	LastErrorType           string `json:"last_error_type"`
	LastErrorCode           string `json:"last_error_code"`
	LastRequestEventAt      int64  `json:"last_request_event_at"`
	LastRequestEventType    string `json:"last_request_event_type"`
	LastRequestEventMessage string `json:"last_request_event_message"`
	LastRequestStatusCode   int    `json:"last_request_status_code"`
	LastRequestErrorType    string `json:"last_request_error_type"`
	LastRequestErrorCode    string `json:"last_request_error_code"`
	LastProbeEventAt        int64  `json:"last_probe_event_at"`
	LastProbeEventType      string `json:"last_probe_event_type"`
	LastProbeEventMessage   string `json:"last_probe_event_message"`
	LastProbeStatusCode     int    `json:"last_probe_status_code"`
	LastProbeErrorType      string `json:"last_probe_error_type"`
	LastProbeErrorCode      string `json:"last_probe_error_code"`
	RecentProblemEvents     int64  `json:"recent_problem_events"`
}

type channelHealthItem struct {
	ID                 int                           `json:"id"`
	Name               string                        `json:"name"`
	Type               int                           `json:"type"`
	Status             int                           `json:"status"`
	Group              string                        `json:"group"`
	Tag                *string                       `json:"tag,omitempty"`
	Models             string                        `json:"models"`
	BaseURL            string                        `json:"base_url"`
	TestModel          *string                       `json:"test_model,omitempty"`
	TestTime           int64                         `json:"test_time"`
	ResponseTime       int                           `json:"response_time"`
	UsedQuota          int64                         `json:"used_quota"`
	Balance            float64                       `json:"balance"`
	BalanceUpdatedTime int64                         `json:"balance_updated_time"`
	HealthStatus       string                        `json:"health_status"`
	HealthReason       string                        `json:"health_reason"`
	ErrorRate          float64                       `json:"error_rate"`
	Recent             channelHealthLogStat          `json:"recent"`
	Events             channelHealthEventStat        `json:"events"`
	Cooldown           service.ChannelCooldownStatus `json:"cooldown"`
}

type channelHealthEventItem struct {
	ID            int                    `json:"id"`
	CreatedAt     int64                  `json:"created_at"`
	EventType     string                 `json:"event_type"`
	Content       string                 `json:"content"`
	ModelName     string                 `json:"model_name"`
	Group         string                 `json:"group"`
	RequestID     string                 `json:"request_id"`
	StatusCode    int                    `json:"status_code"`
	ErrorType     string                 `json:"error_type"`
	ErrorCode     string                 `json:"error_code"`
	ChannelID     int                    `json:"channel_id"`
	TokenID       int                    `json:"token_id"`
	TokenName     string                 `json:"token_name"`
	Other         map[string]interface{} `json:"other"`
	ProbeEvent    bool                   `json:"probe_event"`
	CooldownEvent bool                   `json:"cooldown_event"`
}

func GetChannelHealth(c *gin.Context) {
	var channels []*model.Channel
	if err := model.DB.Order("priority desc").Omit("key").Find(&channels).Error; err != nil {
		common.SysError("failed to get channel health: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取渠道健康度失败，请稍后重试"})
		return
	}

	channelIds := make([]int, 0, len(channels))
	for _, channel := range channels {
		channelIds = append(channelIds, channel.Id)
	}

	cooldowns := service.GetChannelCooldownStatuses(channelIds)
	recentStats := getChannelRecentLogStats(24 * time.Hour)
	eventStats := getChannelHealthEventStats(24 * time.Hour)
	items := make([]channelHealthItem, 0, len(channels))
	summary := gin.H{
		"total":                 len(channels),
		"operational":           0,
		"degraded":              0,
		"provider_cooling":      0,
		"cooling":               0,
		"disabled":              0,
		"auto_disabled":         0,
		"unobserved":            0,
		"recent_requests":       int64(0),
		"recent_errors":         int64(0),
		"final_errors":          int64(0),
		"failover_errors":       int64(0),
		"provider_cooldowns":    int64(0),
		"newapi_cooldowns":      int64(0),
		"probe_waiting":         int64(0),
		"probe_scanned":         int64(0),
		"probe_skipped":         int64(0),
		"probe_started":         int64(0),
		"probe_failed":          int64(0),
		"probe_succeeded":       int64(0),
		"manual_recovered":      int64(0),
		"recent_problem_events": int64(0),
		"window_seconds":        int64((24 * time.Hour).Seconds()),
		"passive_tracking":      true,
		"active_probe_cost":     common.ChannelCooldownProbeEnabled,
	}

	for _, channel := range channels {
		clearChannelInfo(channel)
		cooldown := cooldowns[channel.Id]
		if cooldown.ChannelID == 0 {
			cooldown.ChannelID = channel.Id
		}
		if activeProbeEnabled, activeProbeMode := service.ChannelActiveProbeEligibility(channel); activeProbeEnabled {
			cooldown.ActiveProbeEnabled = true
			cooldown.ActiveProbeMode = activeProbeMode
		}
		recent := recentStats[channel.Id]
		events := eventStats[channel.Id]
		errorRate := 0.0
		if recent.TotalRequests > 0 {
			errorRate = float64(recent.ErrorRequests+events.FinalErrors) / float64(recent.TotalRequests)
		}
		status, reason := resolveChannelHealthStatus(channel, cooldown, recent, events, errorRate)
		incrementSummary(summary, status)
		summary["recent_requests"] = summary["recent_requests"].(int64) + recent.TotalRequests
		summary["recent_errors"] = summary["recent_errors"].(int64) + recent.ErrorRequests
		summary["final_errors"] = summary["final_errors"].(int64) + events.FinalErrors
		summary["failover_errors"] = summary["failover_errors"].(int64) + events.FailoverErrors
		summary["provider_cooldowns"] = summary["provider_cooldowns"].(int64) + events.ProviderCooldowns
		summary["newapi_cooldowns"] = summary["newapi_cooldowns"].(int64) + events.NewAPICooldowns
		summary["probe_waiting"] = summary["probe_waiting"].(int64) + events.ProbeWaiting
		summary["probe_scanned"] = summary["probe_scanned"].(int64) + events.ProbeScanned
		summary["probe_skipped"] = summary["probe_skipped"].(int64) + events.ProbeSkipped
		summary["probe_started"] = summary["probe_started"].(int64) + events.ProbeStarted
		summary["probe_failed"] = summary["probe_failed"].(int64) + events.ProbeFailed
		summary["probe_succeeded"] = summary["probe_succeeded"].(int64) + events.ProbeSucceeded
		summary["manual_recovered"] = summary["manual_recovered"].(int64) + events.ManualRecovered
		summary["recent_problem_events"] = summary["recent_problem_events"].(int64) + events.RecentProblemEvents

		items = append(items, channelHealthItem{
			ID:                 channel.Id,
			Name:               channel.Name,
			Type:               channel.Type,
			Status:             channel.Status,
			Group:              channel.Group,
			Tag:                channel.Tag,
			Models:             channel.Models,
			BaseURL:            channel.GetBaseURL(),
			TestModel:          channel.TestModel,
			TestTime:           channel.TestTime,
			ResponseTime:       channel.ResponseTime,
			UsedQuota:          channel.UsedQuota,
			Balance:            channel.Balance,
			BalanceUpdatedTime: channel.BalanceUpdatedTime,
			HealthStatus:       status,
			HealthReason:       reason,
			ErrorRate:          errorRate,
			Recent:             recent,
			Events:             events,
			Cooldown:           cooldown,
		})
	}

	common.ApiSuccess(c, gin.H{
		"items":   items,
		"summary": summary,
		"settings": gin.H{
			"automatic_channel_cooldown_enabled":     common.AutomaticChannelCooldownEnabled,
			"channel_cooldown_failure_threshold":     common.ChannelCooldownFailureThreshold,
			"channel_cooldown_failure_window":        common.ChannelCooldownFailureWindowSeconds,
			"channel_cooldown_seconds":               common.ChannelCooldownSeconds,
			"channel_cooldown_probe_enabled":         common.ChannelCooldownProbeEnabled,
			"channel_cooldown_probe_interval":        common.ChannelCooldownProbeIntervalSeconds,
			"channel_cooldown_probe_timeout":         common.ChannelCooldownProbeTimeoutSeconds,
			"automatic_disable_channel_enabled":      common.AutomaticDisableChannelEnabled,
			"automatic_enable_channel_enabled":       common.AutomaticEnableChannelEnabled,
			"channel_disable_threshold_seconds":      common.ChannelDisableThreshold,
			"channel_cooldown_probe_scope":           "enabled channels with numeric rate suffix or explicit active_probe_enabled=true",
			"channel_cooldown_probe_recent_window":   int64((time.Hour).Seconds()),
			"channel_cooldown_continuous_probe":      true,
			"health_recent_window_seconds":           int64((24 * time.Hour).Seconds()),
			"page_load_consumes_upstream_quota":      false,
			"manual_test_consumes_upstream_quota":    true,
			"cooldown_probe_consumes_upstream_quota": true,
		},
	})
}

func GetChannelHealthEvents(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorMsg(c, "无效的渠道ID")
		return
	}
	limit := parseBoundedPositiveInt(c.Query("limit"), 100, 500)
	hours := parseBoundedPositiveInt(c.Query("hours"), 24, 168)
	probeOnly := strings.EqualFold(c.DefaultQuery("probe_only", "false"), "true")
	scope := normalizeChannelHealthEventScope(c.Query("scope"), probeOnly)
	since := common.GetTimestamp() - int64(hours)*3600
	scopeWhere, scopeArgs := channelHealthEventScopeWhere(scope)
	queryLimit := limit

	logs := make([]*model.Log, 0)
	query := model.LOG_DB.Model(&model.Log{}).
		Where("channel_id = ? AND created_at >= ? AND type = ? AND other LIKE ?", channelID, since, model.LogTypeSystem, "%\"health_event\":true%")
	if scopeWhere != "" {
		query = query.Where(scopeWhere, scopeArgs...)
	}
	err = query.
		Order("created_at desc, id desc").
		Limit(queryLimit).
		Find(&logs).Error
	if err != nil {
		common.SysError("failed to get channel health event details: " + err.Error())
		common.ApiErrorMsg(c, "获取渠道健康事件失败")
		return
	}

	items := make([]channelHealthEventItem, 0, len(logs))
	for _, log := range logs {
		if log == nil {
			continue
		}
		other := map[string]interface{}{}
		if err := json.Unmarshal([]byte(log.Other), &other); err != nil {
			continue
		}
		if healthy, _ := other["health_event"].(bool); !healthy {
			continue
		}
		eventType := stringFromHealthEvent(other["event_type"])
		if !channelHealthEventMatchesScope(eventType, scope) {
			continue
		}
		delete(other, "admin_info")
		items = append(items, channelHealthEventItem{
			ID:            log.Id,
			CreatedAt:     log.CreatedAt,
			EventType:     eventType,
			Content:       log.Content,
			ModelName:     log.ModelName,
			Group:         log.Group,
			RequestID:     log.RequestId,
			StatusCode:    intFromHealthEvent(other["status_code"]),
			ErrorType:     stringFromHealthEvent(other["error_type"]),
			ErrorCode:     stringFromHealthEvent(other["error_code"]),
			ChannelID:     log.ChannelId,
			TokenID:       log.TokenId,
			TokenName:     log.TokenName,
			Other:         other,
			ProbeEvent:    isChannelHealthProbeEvent(eventType),
			CooldownEvent: isChannelHealthCooldownEvent(eventType),
		})
		if len(items) >= limit {
			break
		}
	}

	common.ApiSuccess(c, gin.H{
		"items": items,
		"meta": gin.H{
			"channel_id":  channelID,
			"hours":       hours,
			"limit":       limit,
			"probe_only":  probeOnly,
			"scope":       scope,
			"window_from": since,
		},
	})
}

func channelHealthEventScopeWhere(scope string) (string, []interface{}) {
	var eventTypes []string
	switch scope {
	case "probe":
		eventTypes = []string{
			service.ChannelHealthEventNewAPICooling,
			service.ChannelHealthEventProbeScanned,
			service.ChannelHealthEventProbeSkipped,
			service.ChannelHealthEventProbeWaiting,
			service.ChannelHealthEventProbeStarted,
			service.ChannelHealthEventProbeFailed,
			service.ChannelHealthEventProbeSucceeded,
			service.ChannelHealthEventManualRecovered,
		}
	case "request":
		eventTypes = []string{
			service.ChannelHealthEventFinalError,
			service.ChannelHealthEventIntermediateFailover,
			service.ChannelHealthEventProviderCooldown,
		}
	default:
		return "", nil
	}
	clauses := make([]string, 0, len(eventTypes))
	args := make([]interface{}, 0, len(eventTypes))
	for _, eventType := range eventTypes {
		clauses = append(clauses, "other LIKE ?")
		args = append(args, fmt.Sprintf("%%\"event_type\":\"%s\"%%", eventType))
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}

func RecoverChannelHealthCooldown(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorMsg(c, "无效的渠道ID")
		return
	}
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		common.ApiErrorMsg(c, "渠道不存在")
		return
	}
	previous := service.RecoverChannelCooldown(channelID)
	service.RecordChannelCooldownHealthEvent(
		service.ChannelHealthEventManualRecovered,
		channelID,
		channel.Name,
		previous.ProbeModel,
		"管理员手动清除 NewAPI 渠道冷却/探针状态",
		map[string]interface{}{
			"cooling_down_before":         previous.CoolingDown,
			"probe_required_before":       previous.ProbeRequired,
			"probing_before":              previous.Probing,
			"failure_count_before":        previous.FailureCount,
			"cooldown_ttl_seconds_before": previous.CooldownTTLSeconds,
			"last_error_before":           previous.LastError,
			"manual_recovery_scope":       "newapi_channel_cooldown",
			"admin_disable_untouched":     true,
			"channel_status":              channel.Status,
		},
	)

	common.ApiSuccess(c, gin.H{
		"channel_id":              channelID,
		"previous":                previous,
		"affected_scope":          "newapi_channel_cooldown",
		"admin_disable_untouched": true,
		"channel_status":          channel.Status,
	})
}

func getChannelRecentLogStats(window time.Duration) map[int]channelHealthLogStat {
	stats := make([]channelHealthLogStat, 0)
	since := common.GetTimestamp() - int64(window.Seconds())
	err := model.LOG_DB.Model(&model.Log{}).
		Select(
			"channel_id, COUNT(*) AS total_requests, SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) AS error_requests, AVG(NULLIF(use_time, 0)) AS avg_use_time, MAX(created_at) AS last_seen_at",
			model.LogTypeError,
		).
		Where("channel_id != 0 AND created_at >= ? AND type IN ?", since, []int{model.LogTypeConsume, model.LogTypeError}).
		Group("channel_id").
		Scan(&stats).Error
	if err != nil {
		common.SysError("failed to get channel health log stats: " + err.Error())
		return map[int]channelHealthLogStat{}
	}
	result := make(map[int]channelHealthLogStat, len(stats))
	for _, stat := range stats {
		result[stat.ChannelId] = stat
	}
	return result
}

func getChannelHealthEventStats(window time.Duration) map[int]channelHealthEventStat {
	since := common.GetTimestamp() - int64(window.Seconds())
	logs := make([]*model.Log, 0)
	err := model.LOG_DB.Model(&model.Log{}).
		Where("channel_id != 0 AND created_at >= ? AND type = ? AND other LIKE ?", since, model.LogTypeSystem, "%\"health_event\":true%").
		Order("id desc").
		Limit(10000).
		Find(&logs).Error
	if err != nil {
		common.SysError("failed to get channel health events: " + err.Error())
		return map[int]channelHealthEventStat{}
	}
	result := make(map[int]channelHealthEventStat)
	for _, log := range logs {
		if log == nil || log.ChannelId <= 0 {
			continue
		}
		other := map[string]interface{}{}
		if err := json.Unmarshal([]byte(log.Other), &other); err != nil {
			continue
		}
		if healthy, _ := other["health_event"].(bool); !healthy {
			continue
		}
		eventType, _ := other["event_type"].(string)
		stat := result[log.ChannelId]
		if stat.ChannelID == 0 {
			stat.ChannelID = log.ChannelId
		}
		switch eventType {
		case service.ChannelHealthEventFinalError:
			stat.FinalErrors++
			stat.RecentProblemEvents++
		case service.ChannelHealthEventIntermediateFailover:
			stat.FailoverErrors++
			stat.RecentProblemEvents++
		case service.ChannelHealthEventProviderCooldown:
			stat.ProviderCooldowns++
			stat.RecentProblemEvents++
		case service.ChannelHealthEventNewAPICooling:
			stat.NewAPICooldowns++
			stat.RecentProblemEvents++
		case service.ChannelHealthEventProbeWaiting:
			stat.ProbeWaiting++
		case service.ChannelHealthEventProbeScanned:
			stat.ProbeScanned++
		case service.ChannelHealthEventProbeSkipped:
			stat.ProbeSkipped++
		case service.ChannelHealthEventProbeStarted:
			stat.ProbeStarted++
		case service.ChannelHealthEventProbeFailed:
			stat.ProbeFailed++
			stat.RecentProblemEvents++
		case service.ChannelHealthEventProbeSucceeded:
			stat.ProbeSucceeded++
		case service.ChannelHealthEventManualRecovered:
			stat.ManualRecovered++
		default:
			continue
		}
		if log.CreatedAt > stat.LastEventAt {
			stat.LastEventAt = log.CreatedAt
			stat.LastEventType = eventType
			stat.LastEventMessage = log.Content
			stat.LastStatusCode = intFromHealthEvent(other["status_code"])
			stat.LastErrorType, _ = other["error_type"].(string)
			stat.LastErrorCode, _ = other["error_code"].(string)
		}
		if isChannelHealthRequestEvent(eventType) && log.CreatedAt > stat.LastRequestEventAt {
			stat.LastRequestEventAt = log.CreatedAt
			stat.LastRequestEventType = eventType
			stat.LastRequestEventMessage = log.Content
			stat.LastRequestStatusCode = intFromHealthEvent(other["status_code"])
			stat.LastRequestErrorType, _ = other["error_type"].(string)
			stat.LastRequestErrorCode, _ = other["error_code"].(string)
		}
		if isChannelHealthProbeScopeEvent(eventType) && log.CreatedAt > stat.LastProbeEventAt {
			stat.LastProbeEventAt = log.CreatedAt
			stat.LastProbeEventType = eventType
			stat.LastProbeEventMessage = log.Content
			stat.LastProbeStatusCode = intFromHealthEvent(other["status_code"])
			stat.LastProbeErrorType, _ = other["error_type"].(string)
			stat.LastProbeErrorCode, _ = other["error_code"].(string)
		}
		result[log.ChannelId] = stat
	}
	return result
}

func parseBoundedPositiveInt(raw string, fallback int, maxValue int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	if maxValue > 0 && value > maxValue {
		return maxValue
	}
	return value
}

func intFromHealthEvent(value interface{}) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func stringFromHealthEvent(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func normalizeChannelHealthEventScope(raw string, probeOnly bool) string {
	if probeOnly {
		return "probe"
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "probe", "request", "all":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "all"
	}
}

func channelHealthEventMatchesScope(eventType string, scope string) bool {
	switch scope {
	case "probe":
		return isChannelHealthProbeScopeEvent(eventType)
	case "request":
		return isChannelHealthRequestEvent(eventType)
	default:
		return true
	}
}

func isChannelHealthProbeEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "probe_")
}

func isChannelHealthProbeScopeEvent(eventType string) bool {
	return isChannelHealthProbeEvent(eventType) ||
		eventType == service.ChannelHealthEventNewAPICooling ||
		eventType == service.ChannelHealthEventManualRecovered
}

func isChannelHealthRequestEvent(eventType string) bool {
	switch eventType {
	case service.ChannelHealthEventFinalError,
		service.ChannelHealthEventIntermediateFailover,
		service.ChannelHealthEventProviderCooldown:
		return true
	default:
		return false
	}
}

func isChannelHealthCooldownEvent(eventType string) bool {
	switch eventType {
	case service.ChannelHealthEventNewAPICooling,
		service.ChannelHealthEventProbeScanned,
		service.ChannelHealthEventProbeSkipped,
		service.ChannelHealthEventProbeWaiting,
		service.ChannelHealthEventProbeStarted,
		service.ChannelHealthEventProbeFailed,
		service.ChannelHealthEventProbeSucceeded,
		service.ChannelHealthEventManualRecovered:
		return true
	default:
		return false
	}
}

func resolveChannelHealthStatus(channel *model.Channel, cooldown service.ChannelCooldownStatus, recent channelHealthLogStat, events channelHealthEventStat, errorRate float64) (string, string) {
	if channel.Status == common.ChannelStatusManuallyDisabled {
		return "disabled", "手动禁用"
	}
	if channel.Status == common.ChannelStatusAutoDisabled {
		return "disabled", "自动禁用"
	}
	if cooldown.CoolingDown || cooldown.ProbeRequired {
		if cooldown.ProbeRequired {
			return "cooling", "当前冷却中，等待主动探针恢复"
		}
		return "cooling", "当前冷却中"
	}
	return "operational", "当前未禁用，未冷却；探针通过后显示正常"
}

func incrementSummary(summary gin.H, status string) {
	switch status {
	case "operational":
		summary["operational"] = summary["operational"].(int) + 1
	case "degraded":
		summary["degraded"] = summary["degraded"].(int) + 1
	case "provider_cooling":
		summary["provider_cooling"] = summary["provider_cooling"].(int) + 1
	case "cooling":
		summary["cooling"] = summary["cooling"].(int) + 1
	case "disabled":
		summary["disabled"] = summary["disabled"].(int) + 1
	case "auto_disabled":
		summary["auto_disabled"] = summary["auto_disabled"].(int) + 1
	default:
		summary["unobserved"] = summary["unobserved"].(int) + 1
	}
}
