package service

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	ChannelHealthEventIntermediateFailover = "intermediate_failover_error"
	ChannelHealthEventFinalError           = "final_error"
	ChannelHealthEventProviderCooldown     = "provider_cooldown"
	ChannelHealthEventNewAPICooling        = "newapi_channel_cooling"
	ChannelHealthEventProbeWaiting         = "probe_waiting"
	ChannelHealthEventProbeScanned         = "probe_scanned"
	ChannelHealthEventProbeSkipped         = "probe_skipped"
	ChannelHealthEventProbeStarted         = "probe_started"
	ChannelHealthEventProbeFailed          = "probe_failed"
	ChannelHealthEventProbeSucceeded       = "probe_succeeded"
	ChannelHealthEventManualRecovered      = "manual_recovered"
)

type ChannelHealthEventParams struct {
	EventType   string
	ChannelID   int
	ChannelName string
	ChannelType int
	UserID      int
	TokenID     int
	TokenName   string
	ModelName   string
	Group       string
	RequestID   string
	StatusCode  int
	ErrorType   string
	ErrorCode   string
	Content     string
	Other       map[string]interface{}
}

func RecordRelayChannelHealthEvent(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	if err == nil || channelError.ChannelId <= 0 {
		return
	}
	eventType := ChannelHealthEventIntermediateFailover
	if err.StatusCode == http.StatusTooManyRequests || IsTransientCredentialCooldownError(err) || IsTransientAuthUnavailableError(err) || IsTransientProviderCooldownError(err) {
		eventType = ChannelHealthEventProviderCooldown
	}
	other := baseRelayChannelHealthEventOther(c, channelError.ChannelId, channelError.ChannelName, channelError.ChannelType)
	recordChannelHealthEvent(ChannelHealthEventParams{
		EventType:   eventType,
		ChannelID:   channelError.ChannelId,
		ChannelName: channelError.ChannelName,
		ChannelType: channelError.ChannelType,
		UserID:      c.GetInt("id"),
		TokenID:     c.GetInt("token_id"),
		TokenName:   c.GetString("token_name"),
		ModelName:   c.GetString("original_model"),
		Group:       c.GetString("group"),
		RequestID:   c.GetString(common.RequestIdKey),
		StatusCode:  err.StatusCode,
		ErrorType:   string(err.GetErrorType()),
		ErrorCode:   string(err.GetErrorCode()),
		Content:     err.MaskSensitiveErrorWithStatusCode(),
		Other:       other,
	})
}

func RecordFinalChannelHealthError(c *gin.Context, err *types.NewAPIError) {
	if c == nil || err == nil || !types.IsRecordErrorLog(err) {
		return
	}
	channelID := c.GetInt("channel_id")
	if channelID <= 0 {
		return
	}
	channelName := c.GetString("channel_name")
	channelType := c.GetInt("channel_type")
	other := baseRelayChannelHealthEventOther(c, channelID, channelName, channelType)
	other["relay_channel_error_seen"] = c.GetBool("relay_channel_error_seen")
	recordChannelHealthEvent(ChannelHealthEventParams{
		EventType:   ChannelHealthEventFinalError,
		ChannelID:   channelID,
		ChannelName: channelName,
		ChannelType: channelType,
		UserID:      c.GetInt("id"),
		TokenID:     c.GetInt("token_id"),
		TokenName:   c.GetString("token_name"),
		ModelName:   c.GetString("original_model"),
		Group:       c.GetString("group"),
		RequestID:   c.GetString(common.RequestIdKey),
		StatusCode:  err.StatusCode,
		ErrorType:   string(err.GetErrorType()),
		ErrorCode:   string(err.GetErrorCode()),
		Content:     err.MaskSensitiveErrorWithStatusCode(),
		Other:       other,
	})
}

func RecordChannelCooldownHealthEvent(eventType string, channelID int, channelName string, modelName string, content string, other map[string]interface{}) {
	if channelID <= 0 || strings.TrimSpace(eventType) == "" {
		return
	}
	recordChannelHealthEvent(ChannelHealthEventParams{
		EventType:   eventType,
		ChannelID:   channelID,
		ChannelName: channelName,
		ModelName:   modelName,
		Content:     content,
		Other:       other,
	})
}

func baseRelayChannelHealthEventOther(c *gin.Context, channelID int, channelName string, channelType int) map[string]interface{} {
	other := map[string]interface{}{
		"request_path": "",
		"channel_id":   channelID,
		"channel_name": channelName,
		"channel_type": channelType,
	}
	if c != nil && c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	adminInfo := map[string]interface{}{
		"use_channel": c.GetStringSlice("use_channel"),
	}
	if common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
		adminInfo["is_multi_key"] = true
		adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	}
	AppendChannelAffinityAdminInfo(c, adminInfo)
	other["admin_info"] = adminInfo
	return other
}

func recordChannelHealthEvent(params ChannelHealthEventParams) {
	eventType := strings.TrimSpace(params.EventType)
	if eventType == "" || params.ChannelID <= 0 {
		return
	}
	other := map[string]interface{}{
		"health_event": true,
		"event_type":   eventType,
	}
	for key, value := range params.Other {
		other[key] = value
	}
	if params.StatusCode != 0 {
		other["status_code"] = params.StatusCode
	}
	if params.ErrorType != "" {
		other["error_type"] = params.ErrorType
	}
	if params.ErrorCode != "" {
		other["error_code"] = params.ErrorCode
	}
	if params.ChannelName != "" {
		other["channel_name"] = params.ChannelName
	}
	if params.ChannelType != 0 {
		other["channel_type"] = params.ChannelType
	}
	log := &model.Log{
		UserId:    params.UserID,
		CreatedAt: common.GetTimestamp(),
		Type:      model.LogTypeSystem,
		Content:   params.Content,
		TokenName: params.TokenName,
		ModelName: params.ModelName,
		ChannelId: params.ChannelID,
		TokenId:   params.TokenID,
		Group:     params.Group,
		RequestId: params.RequestID,
		Other:     common.MapToJsonStr(other),
	}
	if err := model.LOG_DB.Create(log).Error; err != nil {
		common.SysError("failed to record channel health event: " + err.Error())
	}
}
