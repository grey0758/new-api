package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cancelTestAdaptor struct {
	Adaptor
	url string
}

func (a cancelTestAdaptor) GetRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.url + "/v1/responses", nil
}
func (a cancelTestAdaptor) SetupRequestHeader(_ *gin.Context, _ *http.Header, _ *relaycommon.RelayInfo) error {
	return nil
}

// A real HTTP transport must cancel both a pending response and a blocked body
// read. A mocked RoundTripper would not prove the upstream connection is closed.
func TestOuterToolsRequestCancellation(t *testing.T) {
	service.InitHttpClient()
	for _, headers := range []bool{false, true} {
		name := "before_headers"
		if headers {
			name = "silent_body"
		}
		t.Run(name, func(t *testing.T) {
			entered, gone, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if headers {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: first\n\n")
					w.(http.Flusher).Flush()
				}
				close(entered)
				select {
				case <-r.Context().Done():
					close(gone)
				case <-release:
				}
			}))
			defer upstream.Close()
			defer close(release)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			info := &relaycommon.RelayInfo{IsStream: true, DisablePing: true, RelayMode: relayconstant.RelayModeResponses,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: upstream.URL + "/outer-tools"}}
			done := make(chan error, 1)
			go func() {
				resp, err := DoApiRequest(cancelTestAdaptor{url: upstream.URL}, c, info, strings.NewReader("{}"))
				if resp != nil {
					defer resp.Body.Close()
					_, err = io.Copy(io.Discard, resp.Body)
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("upstream not entered")
			}
			cancel()
			select {
			case <-gone:
			case <-time.After(time.Second):
				t.Error("canceled caller left silent upstream running")
			}
			select {
			case err := <-done:
				require.Error(t, err)
			case <-time.After(time.Second):
				t.Error("request did not return after cancellation")
			}
		})
	}
}
