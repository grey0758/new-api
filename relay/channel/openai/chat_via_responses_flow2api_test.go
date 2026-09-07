package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newFlow2APITestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, recorder
}

func flow2APIHTTPResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestOaiResponsesStreamToChatHandlerPropagatesFlow2APIError(t *testing.T) {
	c, recorder := newFlow2APITestContext()
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-3.0-pro-image-square-4k"}}
	body := "data: {\"error\":{\"message\":\"当前模型需要 Ult 账号，但没有可用的 Ult 账号\",\"type\":\"server_error\",\"code\":\"generation_failed\",\"status_code\":503}}\n\n"
	usage, err := OaiResponsesStreamToChatHandler(c, info, flow2APIHTTPResponse(body))
	require.Nil(t, usage)
	require.Error(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Equal(t, types.ErrorCode("generation_failed"), err.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesStreamToChatHandlerKeepsFlow2APIImageMarkdown(t *testing.T) {
	c, recorder := newFlow2APITestContext()
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-3.1-flash-image-square-2k"}}
	body := strings.Join([]string{
		"data: {\"id\":\"chatcmpl-flow\",\"object\":\"chat.completion.chunk\",\"model\":\"flow2api\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"缓存 2K 图片中...\"},\"finish_reason\":null}]}",
		"data: {\"id\":\"chatcmpl-flow\",\"object\":\"chat.completion.chunk\",\"model\":\"flow2api\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"![Generated Image](https://flow.opencodex.uk/tmp/example_2K.jpg)\"},\"finish_reason\":\"stop\"}]}",
		"data: [DONE]",
	}, "\n\n")
	usage, err := OaiResponsesStreamToChatHandler(c, info, flow2APIHTTPResponse(body))
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Greater(t, usage.TotalTokens, 0)
	require.Contains(t, recorder.Body.String(), "https://flow.opencodex.uk/tmp/example_2K.jpg")
	require.Contains(t, recorder.Body.String(), "chat.completion")
}

func TestOaiResponsesStreamToChatHandlerRejectsEmptyFlow2APIStream(t *testing.T) {
	c, recorder := newFlow2APITestContext()
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-3.1-flash-image-square-2k"}}
	usage, err := OaiResponsesStreamToChatHandler(c, info, flow2APIHTTPResponse("data: [DONE]\n\n"))
	require.Nil(t, usage)
	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, types.ErrorCodeEmptyResponse, err.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}
