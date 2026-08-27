package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteWebSearchResponsePreservesSchemaAndBuildsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}}
	info.SetEstimatePromptTokens(37)
	body := `{"encrypted_output":"opaque","output":"search result"}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, newErr := writeWebSearchResponse(c, resp, info)

	require.Nil(t, newErr)
	require.Equal(t, body, recorder.Body.String())
	require.Equal(t, 37, usage.PromptTokens)
	require.Positive(t, usage.CompletionTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

func TestWriteWebSearchResponseRejectsEmptyOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"encrypted_output":null,"output":""}`)),
	}

	usage, newErr := writeWebSearchResponse(c, resp, info)

	require.Nil(t, usage)
	require.NotNil(t, newErr)
	require.Equal(t, http.StatusBadGateway, newErr.StatusCode)
}
