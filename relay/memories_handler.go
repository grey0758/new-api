package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// MemoriesHelper preserves Codex's memory summarize wire contract while
// routing and billing it through the selected NewAPI channel.
func MemoriesHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)
	request, ok := info.Request.(*dto.OpenAIMemorySummarizeRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid memories request type %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	requestBody, err := common.Marshal(request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	respAny, err := adaptor.DoRequest(c, info, bytes.NewReader(requestBody))
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	resp, ok := respAny.(*http.Response)
	if !ok || resp == nil {
		return types.NewError(errors.New("invalid memories upstream response"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
	}
	if resp.StatusCode != http.StatusOK {
		return service.RelayErrorHandler(c.Request.Context(), resp, false)
	}
	usage, newErr := writeMemoriesResponse(c, resp, info, len(request.Traces))
	if newErr != nil {
		return newErr
	}
	service.PostTextConsumeQuota(c, info, usage, nil)
	return nil
}

func writeMemoriesResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, expected int) (*dto.Usage, *types.NewAPIError) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	var memoryResponse dto.OpenAIMemorySummarizeResponse
	if err := common.Unmarshal(body, &memoryResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if len(memoryResponse.Output) != expected {
		return nil, types.NewErrorWithStatusCode(errors.New("memory summarize returned the wrong output count"), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	var text strings.Builder
	for _, output := range memoryResponse.Output {
		if strings.TrimSpace(output.TraceSummary) == "" || strings.TrimSpace(output.MemorySummary) == "" {
			return nil, types.NewErrorWithStatusCode(errors.New("memory summarize returned an empty summary"), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
		}
		text.WriteString(output.TraceSummary)
		text.WriteByte('\n')
		text.WriteString(output.MemorySummary)
		text.WriteByte('\n')
	}
	service.IOCopyBytesGracefully(c, resp, body)
	usage := memoryResponse.Usage
	if usage != nil && usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
		usage.PromptTokens = usage.InputTokens
		usage.CompletionTokens = usage.OutputTokens
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if !service.ValidUsage(usage) {
		usage = service.ResponseText2Usage(c, text.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	return usage, nil
}
