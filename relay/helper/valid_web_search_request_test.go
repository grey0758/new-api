package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidateWebSearchRequestPreservesExplicitZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{
		"id":"search_1",
		"model":"gpt-5.6-sol",
		"input":{"type":"computer_initialize_state"},
		"commands":[],
		"settings":{},
		"max_output_tokens":0
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateWebSearchRequest(ctx)

	require.NoError(t, err)
	require.Equal(t, "search_1", request.ID)
	require.Equal(t, "gpt-5.6-sol", request.Model)
	require.NotNil(t, request.MaxOutputTokens)
	require.Zero(t, *request.MaxOutputTokens)
}

func TestGetAndValidateWebSearchRequestRequiresIDAndModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, testCase := range map[string]struct {
		body      string
		wantError string
	}{
		"missing id":    {body: `{"model":"gpt-5.6-sol"}`, wantError: "id is required"},
		"missing model": {body: `{"id":"search_1"}`, wantError: "model is required"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(testCase.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			_, err := GetAndValidateWebSearchRequest(ctx)

			require.EqualError(t, err, testCase.wantError)
		})
	}
}
