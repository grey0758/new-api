package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetModelRequestKeepsCompactSelectionSuffix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, shouldSelect, err := getModelRequest(ctx)

	require.NoError(t, err)
	require.True(t, shouldSelect)
	require.Equal(t, "gpt-5.6-sol"+ratio_setting.CompactModelSuffix, request.Model)
}

func TestResponsesCompactBaseModelFallbackIsRouteScoped(t *testing.T) {
	compactModel := "gpt-5.6-sol" + ratio_setting.CompactModelSuffix

	baseModel, enabled := responsesCompactBaseModel("/v1/responses/compact", compactModel)
	require.True(t, enabled)
	require.Equal(t, "gpt-5.6-sol", baseModel)
	require.Equal(t, "gpt-5.6-sol", requestModelLimitMatchName("/v1/responses/compact", compactModel))

	_, enabled = responsesCompactBaseModel("/v1/responses", compactModel)
	require.False(t, enabled)
	require.Equal(t, compactModel, requestModelLimitMatchName("/v1/responses", compactModel))
}

func TestResponsesCompactBaseModelFallbackRequiresChannelOptIn(t *testing.T) {
	enabled := &model.Channel{OtherSettings: `{"responses_compact_base_model_fallback":true}`}
	disabled := &model.Channel{OtherSettings: `{}`}

	require.True(t, channelAllowsResponsesCompactBaseModelFallback(enabled))
	require.False(t, channelAllowsResponsesCompactBaseModelFallback(disabled))
	require.False(t, channelAllowsResponsesCompactBaseModelFallback(nil))
}
