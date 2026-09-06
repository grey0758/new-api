package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOuterToolsChannelIsFilteredBeforeChatDistribution(t *testing.T) {
	baseURL := " http://10.253.0.1:18789/outer-tools/ "
	channel := &model.Channel{BaseURL: &baseURL}
	unsupportedSeen := false

	require.False(t, channelAllowedForDistribution("/v1/chat/completions", channel, &unsupportedSeen))
	require.True(t, unsupportedSeen)

	unsupportedSeen = false
	require.True(t, channelAllowedForDistribution("/v1/responses", channel, &unsupportedSeen))
	require.False(t, unsupportedSeen)
}

func TestUnsupportedOuterToolsDistributionErrorIsSafeAndStable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set(common.RequestIdKey, "test-request")

	abortUnsupportedChannelEndpoint(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.JSONEq(t, `{"error":{"message":"`+service.OuterToolsResponsesOnlyMessage+` (request id: test-request)","type":"new_api_error","code":"unsupported_channel_endpoint"}}`, recorder.Body.String())
	require.True(t, ctx.IsAborted())
}
