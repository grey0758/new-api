package controller

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestResponsesWebSocketBillingQueuesAreFIFOAndLaneLocal(t *testing.T) {
	billing := newResponsesWebSocketBilling()
	first := &responsesWebSocketBillingEntry{info: &relaycommon.RelayInfo{RequestId: "first"}}
	second := &responsesWebSocketBillingEntry{info: &relaycommon.RelayInfo{RequestId: "second"}}
	other := &responsesWebSocketBillingEntry{info: &relaycommon.RelayInfo{RequestId: "other"}}
	billing.append(responsesWebSocketLaneKey(""), first)
	billing.append(responsesWebSocketLaneKey(""), second)
	billing.append(responsesWebSocketLaneKey("parallel"), other)

	require.Same(t, other, billing.take(responsesWebSocketLaneKey("parallel")))
	require.Same(t, first, billing.take(responsesWebSocketLaneKey("")))
	require.Same(t, second, billing.take(responsesWebSocketLaneKey("")))
	require.Nil(t, billing.take(responsesWebSocketLaneKey("")))
}

func TestResponsesWebSocketWarmupNilBillingKeepsLaneOrder(t *testing.T) {
	billing := newResponsesWebSocketBilling()
	warmup := &responsesWebSocketBillingEntry{}
	paid := &responsesWebSocketBillingEntry{info: &relaycommon.RelayInfo{RequestId: "paid"}}
	billing.append(responsesWebSocketLaneKey("warmup"), warmup)
	billing.append(responsesWebSocketLaneKey("warmup"), paid)

	require.Same(t, warmup, billing.take(responsesWebSocketLaneKey("warmup")))
	require.Same(t, paid, billing.take(responsesWebSocketLaneKey("warmup")))
}

func TestResponsesWebSocketBillingRemovesOnlyFailedWriteEntry(t *testing.T) {
	billing := newResponsesWebSocketBilling()
	lane := responsesWebSocketLaneKey("main")
	first := &responsesWebSocketBillingEntry{info: &relaycommon.RelayInfo{RequestId: "first"}}
	failed := &responsesWebSocketBillingEntry{info: &relaycommon.RelayInfo{RequestId: "failed"}}
	last := &responsesWebSocketBillingEntry{info: &relaycommon.RelayInfo{RequestId: "last"}}
	billing.append(lane, first)
	billing.append(lane, failed)
	billing.append(lane, last)

	require.True(t, billing.remove(lane, failed))
	require.False(t, billing.remove(lane, failed))
	require.Same(t, first, billing.take(lane))
	require.Same(t, last, billing.take(lane))
}

func TestResponsesWebSocketUsagePreservesCacheReadAndWrite(t *testing.T) {
	event := map[string]any{
		"type":      "response.completed",
		"stream_id": "cache-test",
		"response": map[string]any{
			"usage": map[string]any{
				"input_tokens":  2400,
				"output_tokens": 20,
				"total_tokens":  2420,
				"input_tokens_details": map[string]any{
					"cached_tokens":      800,
					"cache_write_tokens": 1200,
				},
			},
		},
	}

	usage := responsesWebSocketUsage(event)
	require.NotNil(t, usage)
	require.Equal(t, 2400, usage.PromptTokens)
	require.Equal(t, 20, usage.CompletionTokens)
	require.Equal(t, 800, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 1200, usage.PromptTokensDetails.CacheWriteTokens)
	require.Equal(t, 1200, usage.PromptTokensDetails.CachedCreationTokens)
	require.Equal(t, "responses_websocket", usage.UsageSource)
}

func TestResponsesWebSocketUsageRejectsEmptyUsage(t *testing.T) {
	usage := responsesWebSocketUsage(map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
				"total_tokens":  0,
			},
		},
	})
	require.Nil(t, usage)
}

func TestResponsesWebSocketDialerCopiesGlobalDefaults(t *testing.T) {
	originalTimeout := websocket.DefaultDialer.HandshakeTimeout
	t.Cleanup(func() { websocket.DefaultDialer.HandshakeTimeout = originalTimeout })
	websocket.DefaultDialer.HandshakeTimeout = 3 * time.Second

	dialer := responsesWebSocketDialer()
	require.Equal(t, 20*time.Second, dialer.HandshakeTimeout)
	require.Equal(t, 3*time.Second, websocket.DefaultDialer.HandshakeTimeout)
}

func TestResponsesWebSocketUpstreamURLPreservesOrdinaryAndOuterToolsBase(t *testing.T) {
	for name, tc := range map[string]struct {
		baseURL string
		want    string
	}{
		"ordinary": {
			baseURL: "http://10.253.0.1:18787",
			want:    "ws://10.253.0.1:18787/v1/responses",
		},
		"outer-tools": {
			baseURL: "http://10.253.0.1:18787/outer-tools",
			want:    "ws://10.253.0.1:18787/outer-tools/v1/responses",
		},
	} {
		t.Run(name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayMode:      relayconstant.RelayModeResponses,
				RequestURLPath: "/v1/responses",
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    constant.ChannelTypeOpenAI,
					ChannelBaseUrl: tc.baseURL,
					ApiType:        constant.APITypeOpenAI,
				},
			}
			got, err := responsesWebSocketUpstreamURL(info)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestResponsesWebSocketSelectedGroupFeedsRelayInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request, _ = http.NewRequest(http.MethodGet, "/v1/responses", nil)
	ctx.Set(string(constant.ContextKeyUsingGroup), "codex-us003-test")
	ctx.Set(string(constant.ContextKeyOriginalModel), "gpt-5.6-sol")

	info := relaycommon.GenRelayInfoResponses(ctx, &dto.OpenAIResponsesRequest{Model: "gpt-5.6-sol"})
	require.Equal(t, "codex-us003-test", info.UsingGroup)
}
