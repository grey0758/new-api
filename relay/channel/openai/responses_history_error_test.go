package openai

import (
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

func TestResponsesHistoryIDStreamStopsWithoutFailover(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "before_headers"
		if committed {
			name = "after_text"
		}
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			info := &relaycommon.RelayInfo{IsStream: true, RelayMode: relayconstant.RelayModeResponses, OriginModelName: "history-id-" + name, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 47}}
			wire := ""
			if committed {
				wire = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"test\"}\n\n"
			}
			wire += "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"server_error\",\"code\":\"unknown_error\",\"message\":\"[ApiIdParam] [input[17].id] [invalid_id_prefix] Invalid synthetic ID\"}}}\n\n"
			_, err := OaiResponsesStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(wire))})
			if committed {
				require.Nil(t, err)
				require.Contains(t, w.Body.String(), `"code":"invalid_prompt"`)
				require.Contains(t, w.Body.String(), `"original_code":"invalid_id_prefix"`)
				require.NotContains(t, w.Body.String(), "Invalid synthetic ID")
			} else {
				require.NotNil(t, err)
				require.Equal(t, 400, err.StatusCode)
				require.Equal(t, service.ResponsesHistoryIDErrorCode, err.GetErrorCode())
				require.False(t, c.Writer.Written())
			}
			require.Empty(t, service.GetResponsesStreamFailoverExcludedChannels(c, info.OriginModelName))
		})
	}
}
