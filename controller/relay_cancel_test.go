package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCanceledOuterToolsTurnDoesNotStartAnotherSelectionCycle(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	err := types.NewErrorWithStatusCode(errors.New("transport stopped"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	info := &relaycommon.RelayInfo{IsStream: true, RelayMode: relayconstant.RelayModeResponses, LastError: err, ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "http://bridge/outer-tools"}}
	require.True(t, shouldStartNextRelayChannelSelectionCycle(c, info, err, 0))
	cancel()
	require.False(t, shouldStartNextRelayChannelSelectionCycle(c, info, err, 0))
}
