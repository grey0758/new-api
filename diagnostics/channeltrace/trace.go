package channeltrace

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const contextKey = "private_channel_trace"

type config struct {
	site     string
	channels map[int]bool
	recorder *recorder
}

var initConfig sync.Once
var active *config

func loadConfig() *config {
	site := strings.ToLower(strings.TrimSpace(os.Getenv("CHANNEL_TRACE_SITE")))
	ids := strings.TrimSpace(os.Getenv("CHANNEL_TRACE_CHANNELS"))
	dir := os.Getenv("CHANNEL_TRACE_DIR")
	if site == "" || ids == "" || dir == "" || strings.ContainsAny(site, "/:@?# ") {
		return nil
	}
	channels := map[int]bool{}
	for _, id := range strings.Split(ids, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(id))
		if err != nil || n <= 0 {
			return nil
		}
		channels[n] = true
	}
	r, err := newRecorder(dir)
	if err != nil {
		common.SysError("channel trace initialization failed (check private directory)")
		return nil
	}
	return &config{site: site, channels: channels, recorder: r}
}
func configured() *config { initConfig.Do(func() { active = loadConfig() }); return active }
func (c *config) matches(req *http.Request) bool {
	if req == nil || req.Method != http.MethodPost {
		return false
	}
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if !strings.EqualFold(host, c.site) {
		return false
	}
	switch req.URL.Path {
	case "/v1/responses", "/v1/responses/compact", "/v1/chat/completions":
		return true
	}
	return false
}

type state struct {
	cfg       *config
	c         *gin.Context
	traceID   string
	requestID string
	started   time.Time
	current   int
	attempt   int
	selected  bool
	writer    *responseWriter
}

func get(c *gin.Context) *state {
	if c == nil {
		return nil
	}
	v, _ := c.Get(contextKey)
	s, _ := v.(*state)
	return s
}
func (s *state) meta(stage string) map[string]any {
	return map[string]any{"schema": 1, "stage": stage, "site": s.cfg.site, "trace_id": s.traceID, "request_id": s.requestID, "channel_id": s.current, "attempt": s.attempt, "elapsed_ms": time.Since(s.started).Milliseconds()}
}

// Begin is passive until Select sees an allowlisted channel. No unselected
// customer's payload, credentials, or response is submitted to the recorder.
func Begin(c *gin.Context) func() {
	cfg := configured()
	if cfg == nil || !cfg.matches(c.Request) {
		return func() {}
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return func() {}
	}
	s := &state{cfg: cfg, c: c, traceID: hex.EncodeToString(id), requestID: c.GetString(common.RequestIdKey), started: time.Now()}
	c.Set(contextKey, s)
	return func() {
		if !s.selected {
			return
		}
		meta := s.meta("client_response")
		meta["status"] = c.Writer.Status()
		meta["headers"] = safeHeaders(c.Writer.Header())
		meta["caller_cancelled"] = c.Request.Context().Err() != nil
		var body *capture
		if s.writer != nil {
			body = s.writer.body.take(s.current != 0 && c.Request.Context().Err() == nil && !s.writer.writeFailed.Load())
		}
		if s.current == 0 {
			meta["body_omitted"] = "final_channel_outside_allowlist"
			if body != nil {
				cfg.recorder.memory.Add(-int64(len(body.data)))
				body = nil
			}
		}
		if s.writer != nil {
			meta["downstream_write_failed"] = s.writer.writeFailed.Load()
			c.Writer = s.writer.ResponseWriter
		}
		cfg.recorder.submit(record{meta: meta, capture: body, contentType: c.Writer.Header().Get("Content-Type")})
	}
}

// Select belongs immediately after channel selection, before conversion/send.
func Select(c *gin.Context, channel int) {
	s := get(c)
	if s == nil {
		return
	}
	s.attempt++
	if !s.cfg.channels[channel] {
		s.current = 0
		return
	}
	s.current = channel
	if !s.selected {
		s.selected = true
		c.Header("X-Channel-Trace-Id", s.traceID)
		s.writer = &responseWriter{ResponseWriter: c.Writer, state: s, body: s.cfg.recorder.capture()}
		c.Writer = s.writer
		meta := s.meta("client_request")
		meta["method"] = c.Request.Method
		meta["path"] = c.Request.URL.Path
		meta["headers"] = safeHeaders(c.Request.Header)
		var body *capture
		if bs, err := common.GetBodyStorage(c); err == nil {
			b := s.cfg.recorder.capture()
			buf := make([]byte, 32<<10)
			for len(b.data) < bodyLimit {
				n, err := bs.Read(buf)
				if n > 0 {
					b.add(buf[:n])
				}
				if err != nil {
					break
				}
				if b.limited {
					break
				}
			}
			b.seen = bs.Size()
			body = b.take(bs.Size() <= bodyLimit && !b.limited)
			if _, err = bs.Seek(0, io.SeekStart); err != nil {
				meta["capture_error"] = "body_seek_failed"
			}
		} else {
			meta["capture_error"] = "body_unavailable"
		}
		s.cfg.recorder.submit(record{meta: meta, capture: body, contentType: c.Request.Header.Get("Content-Type")})
	}
	s.cfg.recorder.submit(record{meta: s.meta("channel_selected")})
}

type responseWriter struct {
	gin.ResponseWriter
	state       *state
	body        *capture
	writeFailed atomic.Bool
}

func (w *responseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if w.state.current != 0 && n > 0 {
		w.body.add(b[:n])
	}
	if err != nil {
		w.writeFailed.Store(true)
	}
	return n, err
}
func (w *responseWriter) WriteString(b string) (int, error) { return w.Write([]byte(b)) }

func Result(c *gin.Context, code string, status int) {
	s := get(c)
	if s == nil || s.current == 0 {
		return
	}
	m := s.meta("attempt_result")
	m["error_code"] = clean(code)
	m["error_status"] = status
	s.cfg.recorder.submit(record{meta: m})
}

type readCapture struct {
	io.ReadCloser
	body     *capture
	finish   func(*capture)
	expected int64
	mu       sync.Mutex
	closed   bool
	eof      bool
	failed   bool
}

func (r *readCapture) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		if n > 0 {
			r.body.add(p[:n])
		}
		if err == io.EOF {
			r.eof = true
		}
		if err != nil && err != io.EOF {
			r.failed = true
		}
	}
	return n, err
}
func (r *readCapture) Close() error {
	err := r.ReadCloser.Close()
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		r.finish(r.body.take(!r.failed && (r.eof || (r.expected >= 0 && r.body.seen == r.expected))))
	}
	return err
}

// HTTP captures the actual post-conversion request and the unmodified upstream
// response while they are consumed. It never reads ahead, retries, or replays.
func HTTP(c *gin.Context, req *http.Request, channel int) func(*http.Response, error) {
	s := get(c)
	if s == nil || s.current == 0 || channel != s.current {
		return func(*http.Response, error) {}
	}
	r := s.cfg.recorder
	meta := s.meta("upstream_request")
	meta["method"] = req.Method
	meta["upstream_host"] = clean(req.URL.Hostname())
	meta["upstream_path"] = clean(req.URL.Path)
	meta["headers"] = safeHeaders(req.Header)
	// Existing bridge trace IDs remain available for the cross-host JSON-RPC join.
	meta["bridge_trace_id"] = clean(req.Header.Get("X-Bridge-Trace-Id"))
	if req.Body != nil {
		ct := req.Header.Get("Content-Type")
		req.Body = &readCapture{ReadCloser: req.Body, body: r.capture(), expected: req.ContentLength, finish: func(b *capture) { r.submit(record{meta: meta, capture: b, contentType: ct}) }}
	} else {
		r.submit(record{meta: meta})
	}
	start := time.Now()
	var mu sync.Mutex
	timing := map[string]int64{}
	mark := func(key string) { mu.Lock(); timing[key] = time.Since(start).Milliseconds(); mu.Unlock() }
	trace := &httptrace.ClientTrace{TLSHandshakeStart: func() { mark("tls_start_ms") }, TLSHandshakeDone: func(tls.ConnectionState, error) { mark("tls_done_ms") }, DNSStart: func(httptrace.DNSStartInfo) { mark("dns_start_ms") }, DNSDone: func(httptrace.DNSDoneInfo) { mark("dns_done_ms") }, ConnectStart: func(string, string) { mark("connect_start_ms") }, ConnectDone: func(string, string, error) { mark("connect_done_ms") }, GotConn: func(httptrace.GotConnInfo) { mark("got_connection_ms") }, GotFirstResponseByte: func() { mark("first_byte_ms") }}
	*req = *req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	responseMeta := s.meta("upstream_response")
	return func(resp *http.Response, err error) {
		responseMeta["headers_ms"] = time.Since(start).Milliseconds()
		mu.Lock()
		copied := map[string]int64{}
		for k, v := range timing {
			copied[k] = v
		}
		mu.Unlock()
		responseMeta["timing"] = copied
		if err != nil {
			origin := "transport"
			if req.Context().Err() != nil {
				origin = "context_cancelled"
			}
			responseMeta["failure_origin"] = origin
			r.submit(record{meta: responseMeta})
			return
		}
		if resp == nil {
			responseMeta["failure_origin"] = "nil_response"
			r.submit(record{meta: responseMeta})
			return
		}
		responseMeta["status"] = resp.StatusCode
		responseMeta["headers"] = safeHeaders(resp.Header)
		if resp.Body == nil {
			r.submit(record{meta: responseMeta})
			return
		}
		ct := resp.Header.Get("Content-Type")
		resp.Body = &readCapture{ReadCloser: resp.Body, body: r.capture(), expected: resp.ContentLength, finish: func(b *capture) {
			responseMeta["total_ms"] = time.Since(start).Milliseconds()
			r.submit(record{meta: responseMeta, capture: b, contentType: ct})
		}}
	}
}
