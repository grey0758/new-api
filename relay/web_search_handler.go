package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// WebSearchHelper forwards Codex's standalone search envelope without
// converting it into a chat or Responses request. Billing still follows the
// normal NewAPI pre-consume/post-consume flow in controller.Relay.
func WebSearchHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)
	request, ok := info.Request.(*dto.OpenAIWebSearchRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid web search request type %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
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
		return types.NewError(errors.New("invalid web search upstream response"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
	}
	if resp.StatusCode != http.StatusOK {
		return service.RelayErrorHandler(c.Request.Context(), resp, false)
	}
	usage, newErr := writeWebSearchResponse(c, resp, info)
	if newErr != nil {
		return newErr
	}
	service.PostTextConsumeQuota(c, info, usage, nil)
	return nil
}

func writeWebSearchResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	var searchResponse dto.OpenAIWebSearchResponse
	if err := common.Unmarshal(body, &searchResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if searchResponse.Output == "" {
		return nil, types.NewErrorWithStatusCode(errors.New("standalone search returned empty output"), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	service.IOCopyBytesGracefully(c, resp, body)
	return service.ResponseText2Usage(c, searchResponse.Output, info.UpstreamModelName, info.GetEstimatePromptTokens()), nil
}
