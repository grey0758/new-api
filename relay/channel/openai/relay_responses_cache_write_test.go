package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesHandlerNormalizesOfficialCacheWriteTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_cache_write",
			"object":"response",
			"status":"completed",
			"output":[],
			"usage":{
				"input_tokens":2400,
				"output_tokens":20,
				"total_tokens":2420,
				"input_tokens_details":{
					"cached_tokens":800,
					"cache_write_tokens":1200
				}
			}
		}`)),
	}

	usage, relayErr := OaiResponsesHandler(ctx, nil, response)

	require.Nil(t, relayErr)
	require.NotNil(t, usage)
	require.Equal(t, 2400, usage.PromptTokens)
	require.Equal(t, 800, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 1200, usage.PromptTokensDetails.CachedCreationTokens)
	require.Contains(t, recorder.Body.String(), `"cache_write_tokens":1200`)
}
