package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteMemoriesResponsePreservesWireBodyAndNormalizesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	body := `{"output":[{"trace_summary":"trace","memory_summary":"memory"}],"usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30}}`
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}
	info.SetEstimatePromptTokens(7)

	usage, err := writeMemoriesResponse(ctx, response, info, 1)

	require.Nil(t, err)
	require.Equal(t, body, recorder.Body.String())
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 10, usage.CompletionTokens)
	require.Equal(t, 30, usage.TotalTokens)
}
