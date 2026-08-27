package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	responsesStreamGuardMaxBufferedEvents = 16
	responsesStreamGuardMaxBufferedBytes  = 64 << 10
)

type responsesStreamBufferedChunk struct {
	data     string
	response dto.ResponsesStreamResponse
}

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if responsesResponse.ID != "" {
		c.Set("relay_response_id", responsesResponse.ID)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CachedCreationTokens = responsesResponse.Usage.InputTokensDetails.NormalizedCacheWriteTokens()
		}
	}
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// 解析 Tools 用量
	for _, tool := range responsesResponse.Tools {
		buildToolinfo, ok := info.ResponsesUsageInfo.BuiltInTools[common.Interface2String(tool["type"])]
		if !ok || buildToolinfo == nil {
			logger.LogError(c, fmt.Sprintf("BuiltInTools not found for tool type: %v", tool["type"]))
			continue
		}
		buildToolinfo.CallCount++
	}
	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	completed := false
	var streamErr *types.NewAPIError
	modelName := responsesStreamModelName(info)
	channelID := responsesStreamChannelID(info)

	guardStream := true
	if !service.ShouldGuardResponsesStream(c, modelName) {
		service.RecordResponsesStreamGuard(c, modelName)
	}
	if guardStream && info != nil {
		originalDisablePing := info.DisablePing
		info.DisablePing = true
		defer func() {
			info.DisablePing = originalDisablePing
		}()
	}
	guardCommitted := !guardStream
	bufferedChunks := make([]responsesStreamBufferedChunk, 0, responsesStreamGuardMaxBufferedEvents)
	bufferedBytes := 0

	flushBufferedChunks := func() {
		for _, chunk := range bufferedChunks {
			sendResponsesStreamData(c, info, chunk.response, chunk.data)
		}
		bufferedChunks = bufferedChunks[:0]
		bufferedBytes = 0
	}

	processStreamResponse := func(streamResponse dto.ResponsesStreamResponse) {
		if streamResponse.Response != nil && streamResponse.Response.ID != "" {
			c.Set("relay_response_id", streamResponse.Response.ID)
		}
		switch streamResponse.Type {
		case "response.completed":
			completed = true
			if streamResponse.Response != nil {
				if streamResponse.Response.Usage != nil {
					if streamResponse.Response.Usage.InputTokens != 0 {
						usage.PromptTokens = streamResponse.Response.Usage.InputTokens
					}
					if streamResponse.Response.Usage.OutputTokens != 0 {
						usage.CompletionTokens = streamResponse.Response.Usage.OutputTokens
					}
					if streamResponse.Response.Usage.TotalTokens != 0 {
						usage.TotalTokens = streamResponse.Response.Usage.TotalTokens
					}
					if streamResponse.Response.Usage.InputTokensDetails != nil {
						usage.PromptTokensDetails.CachedTokens = streamResponse.Response.Usage.InputTokensDetails.CachedTokens
						usage.PromptTokensDetails.CachedCreationTokens = streamResponse.Response.Usage.InputTokensDetails.NormalizedCacheWriteTokens()
					}
				}
				if streamResponse.Response.HasImageGenerationCall() {
					c.Set("image_generation_call", true)
					c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
					c.Set("image_generation_call_size", streamResponse.Response.GetSize())
				}
			}
		case "response.output_text.delta":
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					if info != nil && info.ResponsesUsageInfo != nil && info.ResponsesUsageInfo.BuiltInTools != nil {
						if webSearchTool, exists := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool != nil {
							webSearchTool.CallCount++
						}
					}
				}
			}
		}
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}

		if isResponsesStreamFatalEvent(streamResponse) {
			streamErr = responsesStreamFatalEventNewAPIError(streamResponse)
			service.RecordResponsesStreamGuard(c, modelName)
			if info.SendResponseCount == 0 && c != nil && c.Writer != nil && !c.Writer.Written() {
				sr.Stop(streamErr)
				return
			}
			sendResponsesStreamData(c, info, streamResponse, data)
			sr.Stop(streamErr)
			return
		}

		processStreamResponse(streamResponse)

		if !guardCommitted {
			bufferedChunks = append(bufferedChunks, responsesStreamBufferedChunk{data: data, response: streamResponse})
			bufferedBytes += len(data)
			if shouldCommitResponsesStreamGuard(streamResponse, len(bufferedChunks), bufferedBytes) {
				flushBufferedChunks()
				guardCommitted = true
			}
			return
		}

		sendResponsesStreamData(c, info, streamResponse, data)
	})

	if streamErr != nil {
		if shouldFailoverResponsesStreamFatalError(streamErr) {
			service.RecordResponsesStreamFailover(c, modelName, channelID)
		}
		service.RecordResponsesStreamGuard(c, modelName)
		if info.SendResponseCount == 0 && c != nil && c.Writer != nil && !c.Writer.Written() {
			return nil, streamErr
		}
		logger.LogError(c, streamErr.Error())
	}

	if !completed {
		err := fmt.Errorf("responses stream closed before response.completed: end=%s received=%d sent=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount, info.SendResponseCount)
		service.RecordResponsesStreamFailover(c, modelName, channelID)
		service.RecordResponsesStreamGuard(c, modelName)
		if info.SendResponseCount == 0 && c != nil && c.Writer != nil && !c.Writer.Written() {
			return nil, types.NewOpenAIError(err, types.ErrorCodeResponsesStreamIncomplete, http.StatusServiceUnavailable)
		}
		logger.LogError(c, err.Error())
	}

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return usage, nil
}

func isResponsesStreamFatalEvent(streamResponse dto.ResponsesStreamResponse) bool {
	switch streamResponse.Type {
	case "response.error", "response.failed":
		return true
	default:
		return false
	}
}

func responsesStreamFatalEventError(streamResponse dto.ResponsesStreamResponse) error {
	if streamResponse.Response != nil {
		if oaiErr := streamResponse.Response.GetOpenAIError(); oaiErr != nil {
			message := strings.TrimSpace(oaiErr.Message)
			if message != "" {
				return fmt.Errorf("responses stream error: type=%s code=%v message=%s", streamResponse.Type, oaiErr.Code, message)
			}
		}
	}
	return fmt.Errorf("responses stream error: %s", streamResponse.Type)
}

func responsesStreamFatalEventNewAPIError(streamResponse dto.ResponsesStreamResponse) *types.NewAPIError {
	if streamResponse.Response != nil {
		if oaiErr := streamResponse.Response.GetOpenAIError(); oaiErr != nil {
			candidate := types.WithOpenAIError(*oaiErr, http.StatusServiceUnavailable)
			if service.IsRequestScopedUpstreamRejectionError(candidate) {
				statusCode := http.StatusBadRequest
				if strings.Contains(strings.ToLower(fmt.Sprintf("%v", oaiErr.Code)), "cyber_policy") {
					statusCode = http.StatusForbidden
				}
				return types.WithOpenAIError(*oaiErr, statusCode, types.ErrOptionWithSkipRetry())
			}
			return candidate
		}
	}
	return types.NewOpenAIError(
		responsesStreamFatalEventError(streamResponse),
		types.ErrorCodeBadResponse,
		http.StatusServiceUnavailable,
	)
}

func shouldFailoverResponsesStreamFatalError(err *types.NewAPIError) bool {
	return err != nil && !service.IsRequestScopedUpstreamRejectionError(err)
}

func shouldCommitResponsesStreamGuard(streamResponse dto.ResponsesStreamResponse, bufferedEvents int, bufferedBytes int) bool {
	if streamResponse.Type == "response.completed" {
		return true
	}
	if streamResponse.Type == "response.output_text.delta" && strings.TrimSpace(streamResponse.Delta) != "" {
		return true
	}
	if streamResponse.Type == dto.ResponsesOutputTypeItemDone && streamResponse.Item != nil {
		return true
	}
	return bufferedEvents >= responsesStreamGuardMaxBufferedEvents || bufferedBytes >= responsesStreamGuardMaxBufferedBytes
}

func responsesStreamModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	return info.OriginModelName
}

func responsesStreamChannelID(info *relaycommon.RelayInfo) int {
	if info == nil {
		return 0
	}
	return info.ChannelId
}
