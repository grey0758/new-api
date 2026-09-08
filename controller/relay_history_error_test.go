package controller

import (
	"errors"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHistoryIDErrorPreservesCauseAndStopsSelection(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("relay_channel_error_seen", true)
	raw := types.NewOpenAIError(errors.New("[ApiIdParam] [input[17].id] [invalid_id_prefix] Invalid synthetic ID"), types.ErrorCode("unknown_error"), 503)
	err := service.NormalizeResponsesHistoryIDError(raw)
	info := &relaycommon.RelayInfo{LastError: err, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 47}}
	require.False(t, shouldRetry(c, err, 5, 47))
	require.False(t, shouldStartNextRelayChannelSelectionCycle(c, info, err, 0))
	visible := sanitizeRelayErrorForUser(c, err)
	require.Equal(t, 400, visible.StatusCode)
	require.Equal(t, service.ResponsesHistoryIDErrorCode, visible.GetErrorCode())
	require.Contains(t, visible.ToOpenAIError().Message, "original compatible route")
}
