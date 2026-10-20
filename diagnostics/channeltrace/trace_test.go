package channeltrace

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T) (*recorder, *gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := &recorder{dir: t.TempDir(), queue: make(chan record, 64), clock: time.Now}
	previous := active
	active = &config{site: "api.open-codex.com", channels: map[int]bool{71: true, 81: true}, recorder: r}
	initConfig.Do(func() {})
	t.Cleanup(func() { active = previous })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "https://api.open-codex.com/v1/responses", strings.NewReader(`{"model":"gpt-6.1-sol","input":[{"type":"function_call_output","call_id":"call_1","output":"tool result"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(common.RequestIdKey, "synthetic_request_1")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return r, c, w
}

func TestScopeRequiresExactSiteChannelAndPath(t *testing.T) {
	for _, target := range []string{"https://api.opencodex.uk/v1/responses", "https://api.open-codex.com/v1/images/generations", "https://api.open-codex.com.evil/v1/responses"} {
		t.Run(target, func(t *testing.T) {
			r, c, _ := fixture(t)
			c.Request = httptest.NewRequest("POST", target, nil)
			done := Begin(c)
			Select(c, 81)
			done()
			require.Empty(t, r.queue)
		})
	}
	r, c, _ := fixture(t)
	done := Begin(c)
	Select(c, 47)
	c.Writer.Write([]byte("unselected private content"))
	done()
	require.Empty(t, r.queue)
}
func TestRealHTTPStreamIsUnmodifiedAndJoined(t *testing.T) {
	r, c, w := fixture(t)
	done := Begin(c)
	Select(c, 81)
	payload := `{"model":"gpt-6.1-sol","reasoning":{"context":"all_turns"},"input":[{"type":"function_call","call_id":"call_1","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`
	reply := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"sk-secret_fragment\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"text\":\"done\"}]}]}}\n\n"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		b, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, payload, string(b))
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Request-Id", "synthetic_upstream")
		w.Write([]byte(reply))
	}))
	defer server.Close()
	req, err := http.NewRequest("POST", server.URL, strings.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer private_header_must_not_be_saved")
	finish := HTTP(c, req, 81)
	resp, err := http.DefaultClient.Do(req)
	finish(resp, err)
	require.NoError(t, err)
	c.Writer.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	_, err = io.Copy(c.Writer, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	Result(c, "", 0)
	done()
	require.Equal(t, reply, w.Body.String())
	require.Equal(t, 1, calls)
	stages := map[string]bool{}
	trace := ""
	for len(r.queue) > 0 {
		e := <-r.queue
		stage := e.meta["stage"].(string)
		stages[stage] = true
		if trace == "" {
			trace = e.meta["trace_id"].(string)
		}
		require.Equal(t, trace, e.meta["trace_id"])
		require.Equal(t, 81, e.meta["channel_id"])
		serialized, err := common.Marshal(e.meta)
		require.NoError(t, err)
		require.NotContains(t, string(serialized), "private_header_must_not_be_saved")
		if e.capture != nil {
			body := decode(e.capture.data, e.contentType)
			b, _ := common.Marshal(body)
			require.NotContains(t, string(b), "sk-secret_fragment")
			r.memory.Add(-int64(len(e.capture.data)))
		}
	}
	for _, stage := range []string{"client_request", "channel_selected", "upstream_request", "upstream_response", "attempt_result", "client_response"} {
		require.True(t, stages[stage], stage)
	}
	require.Zero(t, r.memory.Load())
}
func TestSanitizationHandlesNestedSecretsAndPartialPackets(t *testing.T) {
	raw := []byte(`{"authorization":"Bearer a","input":[{"type":"function_call","call_id":"call_keep","arguments":"{\"password\":\"nested_canary\"}"},{"type":"reasoning","encrypted_content":"opaque_canary"}],"output":"TOKEN_API_KEY=assignment_canary https://example.test/file?signature=url_canary","text":"sk-123456789012345678901234567890"}`)
	out, err := common.Marshal(decode(raw, "application/json"))
	require.NoError(t, err)
	for _, secret := range []string{"nested_canary", "opaque_canary", "assignment_canary", "url_canary", "sk-123456789012345678901234567890"} {
		require.NotContains(t, string(out), secret)
	}
	require.Contains(t, string(out), "call_keep")
	out, _ = common.Marshal(decode([]byte(`{"password":"partial_canary`), "application/json"))
	require.NotContains(t, string(out), "partial_canary")
}
func TestPolicyAndTerminalErrorsAreCapturedWithoutRewriting(t *testing.T) {
	for _, code := range []string{"outer_tool_turn_terminated", "content_policy_violation"} {
		t.Run(code, func(t *testing.T) {
			r, c, w := fixture(t)
			done := Begin(c)
			Select(c, 81)
			reply := `{"error":{"code":"` + code + `","message":"synthetic upstream rejection"}}`
			req := httptest.NewRequest("POST", "https://upstream.invalid/v1/responses", nil)
			finish := HTTP(c, req, 81)
			resp := &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": []string{"application/json"}}, ContentLength: int64(len(reply)), Body: io.NopCloser(strings.NewReader(reply))}
			finish(resp, nil)
			c.Writer.WriteHeader(400)
			_, err := io.Copy(c.Writer, resp.Body)
			require.NoError(t, err)
			resp.Body.Close()
			Result(c, code, 400)
			done()
			require.Equal(t, 400, w.Code)
			require.Equal(t, reply, w.Body.String())
			require.Greater(t, len(r.queue), 3)
		})
	}
}
func TestRetentionExpirySizeAndPermissions(t *testing.T) {
	r := &recorder{dir: t.TempDir(), clock: func() time.Time { return time.Date(2026, 10, 20, 18, 30, 0, 0, time.UTC) }}
	old := filepath.Join(r.dir, "trace-20261019T18-100-000001.jsonl.gz")
	fresh := filepath.Join(r.dir, "trace-20261020T18-100-000001.jsonl.gz")
	unrelated := filepath.Join(r.dir, "keep.log")
	for _, p := range []string{old, fresh, unrelated} {
		require.NoError(t, os.WriteFile(p, []byte("test"), 0600))
	}
	require.NoError(t, r.maintain())
	_, err := os.Stat(old)
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(fresh)
	require.NoError(t, err)
	_, err = os.Stat(unrelated)
	require.NoError(t, err)
	require.NoError(t, r.write([]byte("{\"synthetic\":true}\n")))
	f, err := os.Open(r.file)
	require.NoError(t, err)
	defer f.Close()
	z, err := gzip.NewReader(f)
	require.NoError(t, err)
	b, err := io.ReadAll(z)
	require.NoError(t, err)
	require.Equal(t, "{\"synthetic\":true}\n", string(b))
	st, err := os.Stat(r.file)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), st.Mode().Perm())
}
func TestCaptureAndQueueLimitsAreExplicit(t *testing.T) {
	r := &recorder{queue: make(chan record, 1), clock: time.Now}
	b := r.capture()
	b.add(bytes.Repeat([]byte("x"), bodyLimit+32))
	snapshot := b.take(false)
	require.Len(t, snapshot.data, bodyLimit)
	require.Equal(t, int64(bodyLimit+32), snapshot.seen)
	r.submit(record{meta: map[string]any{}})
	r.submit(record{meta: map[string]any{}, capture: snapshot})
	require.EqualValues(t, 1, r.dropped.Load())
	require.Zero(t, r.memory.Load())
	r.memory.Store(memoryLimit)
	b = r.capture()
	b.add([]byte("bounded"))
	require.True(t, b.limited)
	require.Empty(t, b.data)
}

func TestUnmonitoredFallbackResponseIsNotCaptured(t *testing.T) {
	r, c, _ := fixture(t)
	done := Begin(c)
	Select(c, 81)
	Select(c, 47)
	c.Writer.Write([]byte("unmonitored_response_canary"))
	done()
	for len(r.queue) > 0 {
		e := <-r.queue
		if e.meta["stage"] == "client_response" {
			require.Nil(t, e.capture)
			require.Equal(t, "final_channel_outside_allowlist", e.meta["body_omitted"])
		}
		if e.capture != nil {
			require.NotContains(t, string(e.capture.data), "unmonitored_response_canary")
			r.memory.Add(-int64(len(e.capture.data)))
		}
	}
	require.Zero(t, r.memory.Load())
}
func TestInvalidConfigurationFailsClosed(t *testing.T) {
	for _, site := range []string{"", "https://api.open-codex.com", "api.open-codex.com:443"} {
		t.Setenv("CHANNEL_TRACE_SITE", site)
		t.Setenv("CHANNEL_TRACE_CHANNELS", "71,81")
		t.Setenv("CHANNEL_TRACE_DIR", t.TempDir())
		require.Nil(t, loadConfig())
	}
	t.Setenv("CHANNEL_TRACE_SITE", "api.open-codex.com")
	t.Setenv("CHANNEL_TRACE_CHANNELS", "71,*")
	require.Nil(t, loadConfig())
}
func TestCapacityEvictsOnlyOwnedArchives(t *testing.T) {
	r := &recorder{dir: t.TempDir(), clock: func() time.Time { return time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC) }}
	p := filepath.Join(r.dir, "trace-20261020T17-100-000001.jsonl.gz")
	f, err := os.Create(p)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(archiveLimit+1))
	f.Close()
	other := filepath.Join(r.dir, "trace-not-ours.jsonl.gz")
	require.NoError(t, os.WriteFile(other, []byte("preserve"), 0600))
	require.NoError(t, r.maintain())
	_, err = os.Stat(p)
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(other)
	require.NoError(t, err)
}
