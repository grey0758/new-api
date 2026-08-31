package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidateMemorySummarizeRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/memories/trace_summarize", strings.NewReader(`{
		"model":"gpt-5.6-sol",
		"traces":[{"id":"trace-1","metadata":{"source_path":"/tmp/trace.json"},"items":[{"type":"message"}]}],
		"reasoning":{"effort":"high"}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateMemorySummarizeRequest(ctx)

	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", request.Model)
	require.Len(t, request.Traces, 1)
	require.Equal(t, "/tmp/trace.json", request.Traces[0].Metadata.SourcePath)
	require.NotEmpty(t, request.GetTokenCountMeta().CombineText)
}

func TestGetAndValidateMemorySummarizeRequestRejectsDuplicateTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/memories/trace_summarize", strings.NewReader(`{
		"model":"gpt-5.6-sol",
		"traces":[
			{"id":"trace-1","metadata":{"source_path":"/tmp/one"},"items":[{}]},
			{"id":"trace-1","metadata":{"source_path":"/tmp/two"},"items":[{}]}
		]
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	_, err := GetAndValidateMemorySummarizeRequest(ctx)

	require.EqualError(t, err, "traces[1].id is duplicated")
}
