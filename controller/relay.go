package controller

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	case relayconstant.RelayModeWebSearch:
		err = relay.WebSearchHelper(c, info)
	case relayconstant.RelayModeMemories:
		err = relay.MemoriesHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

const responsesCompactMaxRequestBodyBytes int64 = 960_000

// rejectOversizedResponsesCompaction prevents large compact requests from
// entering token estimation or channel processing.  The bridge applies the
// same limit, but this early guard keeps malformed/extreme bodies from
// consuming NewAPI CPU or waiting for an upstream timeout first.
func rejectOversizedResponsesCompaction(c *gin.Context, relayFormat types.RelayFormat) *types.NewAPIError {
	if relayFormat != types.RelayFormatOpenAIResponsesCompaction || c == nil || c.Request == nil {
		return nil
	}
	if c.Request.ContentLength > responsesCompactMaxRequestBodyBytes {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("responses compaction request body exceeds %d bytes", responsesCompactMaxRequestBodyBytes),
			types.ErrorCodeReadRequestBodyFailed,
			http.StatusRequestEntityTooLarge,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, responsesCompactMaxRequestBodyBytes)
	}
	return nil
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", newAPIError.Error()))
			service.RecordFinalChannelHealthError(c, newAPIError)
			newAPIError = sanitizeRelayErrorForUser(c, newAPIError)
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				c.JSON(newAPIError.StatusCode, gin.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				c.JSON(newAPIError.StatusCode, gin.H{
					"error": newAPIError.ToOpenAIError(),
				})
			}
		}
	}()

	if newAPIError = rejectOversizedResponsesCompaction(c, relayFormat); newAPIError != nil {
		return
	}

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}

	imageRequestTracked := false
	if relayFormat == types.RelayFormatOpenAIImage && service.GeneratedAssetsEnabled() {
		imageRequest, ok := request.(*dto.ImageRequest)
		if !ok {
			newAPIError = types.NewError(
				fmt.Errorf("invalid image request type %T", request),
				types.ErrorCodeInvalidRequest,
				types.ErrOptionWithSkipRetry(),
			)
			return
		}
		decision, prepareErr := service.PrepareGeneratedImageRequest(c, relayInfo, imageRequest)
		if prepareErr != nil {
			newAPIError = types.NewErrorWithStatusCode(
				prepareErr,
				types.ErrorCode("generated_asset_runtime_unavailable"),
				http.StatusServiceUnavailable,
				types.ErrOptionWithSkipRetry(),
			)
			return
		}
		switch decision.Action {
		case service.GeneratedImageDecisionProceed:
			imageRequestTracked = true
		case service.GeneratedImageDecisionReplay:
			c.Header("X-Idempotent-Replay", "true")
			c.Header("X-Original-Request-Id", decision.RequestId)
			c.Data(decision.StatusCode, "application/json; charset=utf-8", decision.ResponseBody)
			return
		case service.GeneratedImageDecisionReject:
			c.Header("X-Original-Request-Id", decision.RequestId)
			newAPIError = types.NewErrorWithStatusCode(
				errors.New(decision.ErrorMessage),
				types.ErrorCode(decision.ErrorCode),
				decision.StatusCode,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
			return
		default:
			newAPIError = types.NewErrorWithStatusCode(
				errors.New("invalid generated image idempotency decision"),
				types.ErrorCode("idempotency_internal_error"),
				http.StatusInternalServerError,
				types.ErrOptionWithSkipRetry(),
			)
			return
		}
	}

	defer func() {
		if !imageRequestTracked || newAPIError == nil {
			return
		}
		errorCode := newAPIError.GetErrorCode()
		if errorCode == "" {
			errorCode = "image_generation_failed"
		}
		if err := service.MarkGeneratedImageRequestFailed(relayInfo.RequestId, newAPIError.StatusCode, string(errorCode)); err != nil {
			logger.LogError(c, "failed to record generated image request failure: "+err.Error())
		}
	}()

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needSecurityPromptCapture := service.PromptViolationCaptureEnabled()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needSecurityPromptCapture || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSecurityPromptCapture && meta != nil {
		service.RecordPromptViolationIfDetected(c, relayInfo.OriginModelName, meta.CombineText)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected, record only: %s", strings.Join(words, ", ")))
			service.RecordLocalSensitiveWordsIfDetected(c, relayInfo.OriginModelName, words)
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:                c,
		TokenGroup:         relayInfo.TokenGroup,
		ModelName:          relayInfo.OriginModelName,
		Retry:              common.GetPointer(0),
		ExcludedChannelIds: service.GetResponsesStreamFailoverExcludedChannels(c, relayInfo.OriginModelName),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for cycle := 0; cycle < maxRelayChannelSelectionCycles(c, relayInfo); cycle++ {
		if cycle > 0 {
			retryParam.ResetSelectionCycle()
			logger.LogInfo(c, fmt.Sprintf("渠道选择进入第 %d/%d 轮", cycle+1, maxRelayChannelSelectionCycles(c, relayInfo)))
		}
		for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
			relayInfo.RetryIndex = retryParam.GetRetry()
			channel, channelErr := getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				newAPIError = channelErr
				break
			}

			addUsedChannel(c, channel.Id)
			bodyStorage, bodyErr := common.GetBodyStorage(c)
			if bodyErr != nil {
				// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
				if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
				} else {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
				}
				break
			}
			c.Request.Body = io.NopCloser(bodyStorage)

			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				newAPIError = relay.WssHelper(c, relayInfo)
			case types.RelayFormatClaude:
				newAPIError = relay.ClaudeHelper(c, relayInfo)
			case types.RelayFormatGemini:
				newAPIError = geminiRelayHandler(c, relayInfo)
			default:
				newAPIError = relayHandler(c, relayInfo)
			}

			if newAPIError == nil {
				relayInfo.LastError = nil
				if !service.HasResponsesStreamIncomplete(c) {
					service.ClearResponsesStreamFailover(c, relayInfo.OriginModelName)
				}
				if imageRequestTracked {
					if err := service.MarkGeneratedImageRequestSucceeded(relayInfo.RequestId); err != nil {
						logger.LogError(c, "failed to mark generated image request succeeded: "+err.Error())
					}
				}
				return
			}

			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			relayInfo.LastError = newAPIError

			processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)

			if !shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry(), channel.Id) {
				if shouldStartNextRelayChannelSelectionCycle(c, relayInfo, newAPIError, cycle) {
					retryParam.ExcludeChannel(channel.Id)
				}
				break
			}
			retryParam.ExcludeChannel(channel.Id)
		}
		if !shouldStartNextRelayChannelSelectionCycle(c, relayInfo, newAPIError, cycle) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

const relayChannelSelectionCycles = 2

func maxRelayChannelSelectionCycles(c *gin.Context, info *relaycommon.RelayInfo) int {
	if c == nil || info == nil || info.ChannelMeta == nil {
		return 1
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return 1
	}
	if relayChannelSelectionCycles < 1 {
		return 1
	}
	return relayChannelSelectionCycles
}

func shouldStartNextRelayChannelSelectionCycle(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError, cycle int) bool {
	if cycle+1 >= maxRelayChannelSelectionCycles(c, info) {
		return false
	}
	if c == nil || info == nil || info.ChannelMeta == nil {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	retryErr := err
	if info.LastError != nil {
		retryErr = info.LastError
	}
	if retryErr == nil || types.IsSkipRetryError(retryErr) {
		return false
	}
	if service.IsTransientRelayFailoverError(retryErr) {
		return true
	}
	return shouldRetryRelayError(retryErr)
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return channel, nil
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int, channelId int) bool {
	if openaiErr == nil {
		return false
	}
	if types.IsChannelError(openaiErr) {
		service.ClearCurrentChannelAffinity(c)
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if service.IsClientRequestValidationError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		if service.IsTransientRelayFailoverError(openaiErr) || service.ShouldDisableChannel(openaiErr) || service.IsChannelCoolingDown(channelId) {
			service.ClearCurrentChannelAffinity(c)
		} else {
			return false
		}
	}
	if service.IsTransientRelayFailoverError(openaiErr) {
		return true
	}
	return shouldRetryRelayError(openaiErr)
}

func shouldRetryRelayError(openaiErr *types.NewAPIError) bool {
	if openaiErr == nil {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, err.Error()))
	service.RecordRelayChannelHealthEvent(c, channelError, err)
	if !service.IsClientRequestValidationError(err) && !service.IsRequestScopedUpstreamRejectionError(err) {
		service.RecordUpstream400ViolationCandidate(c, channelError, err)
	}
	c.Set("relay_channel_error_seen", true)
	if !service.IsResponsesStreamIncompleteError(err) {
		service.RecordChannelFailureForCooldownWithActor(channelError, err, c.GetString("original_model"), c.GetInt("id"), c.GetInt("token_id"))
	}
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if service.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		other = service.AppendSecurityAuditToOther(c, other)
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}

}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *dto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *dto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			break
		}

		if !taskErr.LocalError {
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode))
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
		}
		service.LogTaskConsumption(c, relayInfo)

		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:      relayInfo.PriceData.ModelPrice,
			GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:      relayInfo.PriceData.ModelRatio,
			OtherRatios:     relayInfo.PriceData.OtherRatios,
			OriginModelName: relayInfo.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		}
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if insertErr := task.Insert(); insertErr != nil {
			common.SysError("insert task error: " + insertErr.Error())
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

const relayUserVisibleChannelErrorMessage = "号池额度已耗尽正在切换号池，请重试"

var outerToolClientStateMessages = map[types.ErrorCode]string{
	types.ErrorCode("duplicate_tool_output"):        "The outer-tool continuation already contains an output for this call.",
	types.ErrorCode("invalid_tool_output"):          "The outer-tool continuation contains an invalid tool output.",
	types.ErrorCode("prompt_cache_key_required"):    "A prompt cache key is required when outer tools are used.",
	types.ErrorCode("outer_tool_session_not_found"): "The outer-tool session is no longer available; start a new turn.",
	types.ErrorCode("outer_tool_session_conflict"):  "The outer-tool session already has a turn in progress.",
	types.ErrorCode("outer_tool_session_mismatch"):  "The outer-tool session model or tools changed; start a new turn.",
	types.ErrorCode("missing_tool_output"):          "The outer-tool continuation is missing a required tool output.",
}

func outerToolClientStateCode(err *types.NewAPIError) (types.ErrorCode, bool) {
	if err == nil {
		return "", false
	}
	if _, ok := outerToolClientStateMessages[err.GetErrorCode()]; ok {
		return err.GetErrorCode(), true
	}
	if openAIErr, ok := err.RelayError.(types.OpenAIError); ok {
		if openAIErr.Code != nil {
			code := types.ErrorCode(fmt.Sprintf("%v", openAIErr.Code))
			if _, known := outerToolClientStateMessages[code]; known {
				return code, true
			}
		}
	}
	return "", false
}

// respondTaskError 统一输出 Task 错误响应，不向用户透传上游错误内容。
func respondTaskError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = relayUserVisibleChannelErrorMessage
	} else if !taskErr.LocalError && taskErr.StatusCode >= http.StatusInternalServerError {
		taskErr.Message = relayUserVisibleChannelErrorMessage
		taskErr.Code = string(types.ErrorCodeBadResponseStatusCode)
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func sanitizeRelayErrorForUser(c *gin.Context, err *types.NewAPIError) *types.NewAPIError {
	if err == nil {
		return nil
	}
	if code, ok := outerToolClientStateCode(err); ok {
		message := outerToolClientStateMessages[code]
		if err.StatusCode >= http.StatusBadRequest && err.StatusCode < http.StatusInternalServerError {
			return types.NewErrorWithStatusCode(
				errors.New(message),
				code,
				err.StatusCode,
				types.ErrOptionWithSkipRetry(),
			)
		}
	}
	if !shouldHideRelayErrorFromUser(c, err) {
		return err
	}
	statusCode := err.StatusCode
	if statusCode < http.StatusInternalServerError {
		statusCode = http.StatusServiceUnavailable
	}
	return types.NewErrorWithStatusCode(
		errors.New(relayUserVisibleChannelErrorMessage),
		types.ErrorCodeBadResponseStatusCode,
		statusCode,
		types.ErrOptionWithSkipRetry(),
	)
}

func shouldHideRelayErrorFromUser(c *gin.Context, err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	// Request-scoped provider policy rejections are valid client-visible 4xx
	// responses. They must not be rewritten as a generic channel 503 merely
	// because the failed attempt was recorded in the channel health lifecycle.
	if service.IsRequestScopedUpstreamRejectionError(err) || types.ShouldPreserveUserError(err) {
		return false
	}
	if c != nil && c.GetBool("relay_channel_error_seen") {
		return true
	}
	if types.IsChannelError(err) {
		return true
	}
	switch err.GetErrorCode() {
	case types.ErrorCodeGetChannelFailed,
		types.ErrorCodeChannelNoAvailableKey,
		types.ErrorCodeChannelInvalidKey,
		types.ErrorCodeChannelResponseTimeExceeded,
		types.ErrorCodeChannelAwsClientError,
		types.ErrorCodeChannelHeaderOverrideInvalid,
		types.ErrorCodeChannelParamOverrideInvalid,
		types.ErrorCodeChannelModelMappedError,
		types.ErrorCodePromptBlocked,
		types.ErrorCodeModelNotFound,
		types.ErrorCodeEmptyResponse:
		return true
	case types.ErrorCodeDoRequestFailed,
		types.ErrorCodeReadResponseBodyFailed,
		types.ErrorCodeBadResponseStatusCode,
		types.ErrorCodeBadResponse,
		types.ErrorCodeBadResponseBody,
		types.ErrorCodeAwsInvokeError:
		return true
	}
	return false
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *dto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		if service.IsChannelCoolingDown(channelId) && shouldRetryTaskError(taskErr) {
			service.ClearCurrentChannelAffinity(c)
		} else {
			return false
		}
	}
	return shouldRetryTaskError(taskErr)
}

func shouldRetryTaskError(taskErr *dto.TaskError) bool {
	if taskErr == nil {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
