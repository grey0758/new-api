package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOuterToolsCanceledStreamDoesNotMarkProviderFailover(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "upstream_eof"
		if canceled {
			name = "caller_canceled"
		}
		t.Run(name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(name)).WithContext(ctx)
			if canceled {
				cancel()
			}
			info := &relaycommon.RelayInfo{IsStream: true, RelayMode: relayconstant.RelayModeResponses, OriginModelName: "cancel-test", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 68, ChannelBaseUrl: "http://bridge/outer-tools"}}
			resp := &http.Response{Body: io.NopCloser(strings.NewReader(""))}
			usage, err := OaiResponsesStreamHandler(c, info, resp)
			if canceled {
				require.Nil(t, err)
				require.Zero(t, usage.TotalTokens)
				require.Empty(t, service.GetResponsesStreamFailoverExcludedChannels(c, info.OriginModelName))
				require.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
			} else {
				require.NotNil(t, err)
				require.True(t, service.GetResponsesStreamFailoverExcludedChannels(c, info.OriginModelName)[68])
			}
			service.ClearResponsesStreamFailover(c, info.OriginModelName)
		})
	}
}
