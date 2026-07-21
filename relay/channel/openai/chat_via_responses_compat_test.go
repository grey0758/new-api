package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLooksLikeResponsesEventStreamBodyRequiresTerminalEvent(t *testing.T) {
	complete := strings.Join([]string{
		": keep-alive",
		"event: response",
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1"}}`,
		"data: [DONE]",
		"",
	}, "\n")
	require.True(t, looksLikeResponsesEventStreamBody([]byte(complete)))

	incomplete := strings.Join([]string{
		": keep-alive",
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		"",
	}, "\n")
	require.False(t, looksLikeResponsesEventStreamBody([]byte(incomplete)))
	require.False(t, looksLikeResponsesEventStreamBody([]byte(": keep-alive\n")))
	require.False(t, looksLikeResponsesEventStreamBody([]byte(`{"id":"resp_1"}`)))
}

func TestOaiResponsesToChatHandlerFallsBackToMislabeledSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body := strings.Join([]string{
		": upstream keep-alive",
		"event: response",
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.6-sol","created_at":123}}`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.6-sol","created_at":123,"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`,
		"data: [DONE]",
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.6-sol",
		},
	}

	usage, newAPIError := OaiResponsesToChatHandler(c, info, resp)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)
	require.Equal(t, 3, usage.TotalTokens)

	var chatResp dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &chatResp))
	require.Len(t, chatResp.Choices, 1)
	require.Equal(t, "hello", chatResp.Choices[0].Message.StringContent())
}

func TestOaiResponsesToChatHandlerRejectsIncompleteMislabeledSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body := ": keep-alive\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	_, newAPIError := OaiResponsesToChatHandler(c, &relaycommon.RelayInfo{}, resp)
	require.NotNil(t, newAPIError)
	require.Equal(t, http.StatusBadGateway, newAPIError.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, newAPIError.GetErrorCode())
}
