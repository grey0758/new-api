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
	require.Contains(t, visible.ToOpenAIError().Message, "Keep the original session")
}

// A configurable retry status must not replay a terminal tool continuation or
// resubmit an explicit policy rejection to another upstream.
func TestTerminalAndPolicyRejectionsNeverStartAnotherAttempt(t *testing.T) {
	for _, code := range []string{"outer_tool_turn_terminated", "bound_account_unavailable", "content_policy_violation", "cyber_policy"} {
		t.Run(code, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			upstream := types.WithOpenAIError(types.OpenAIError{Code: code, Message: "synthetic provider rejection", Type: "server_error"}, 503)
			info := &relaycommon.RelayInfo{LastError: upstream, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 81}}
			require.False(t, shouldRetry(c, upstream, 5, 81))
			require.False(t, shouldStartNextRelayChannelSelectionCycle(c, info, upstream, 0))
		})
	}
}
