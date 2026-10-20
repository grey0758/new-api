// Package channeltrace records explicitly allowlisted application traffic.
// Archives are diagnostics only and must never be used to replay requests.
package channeltrace

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var credentials = regexp.MustCompile(`(?i)(-----BEGIN [^-\n]*PRIVATE KEY-----[\s\S]*?-----END [^-\n]*PRIVATE KEY-----|\b(?:sk-|github_pat_|gh[pousr]_)[A-Za-z0-9_-]{16,}|\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}|bearer\s+[A-Za-z0-9_.~+/=-]+)`)
var assignments = regexp.MustCompile(`(?i)(\b[\w.-]{0,64}(?:api[_-]?key|password|passwd|secret|access[_-]?token|refresh[_-]?token|authorization)[\w.-]{0,64}["']?\s*[:=]\s*["']?)[^\s,;"'}]+`)
var secretKey = regexp.MustCompile(`(?i)(authorization|cookie|password|passwd|secret|credential|private.?key|api.?key|access.?token|refresh.?token|id.?token)|^(token|key|session_token)$`)
var queryURL = regexp.MustCompile(`(?i)(https?://[^\s"'<>?]+)\?[^\s"'<>]*`)
var binaryData = regexp.MustCompile(`data:[^,\s]{0,160},[A-Za-z0-9+/=_-]+`)

func clean(s string) string {
	s = credentials.ReplaceAllString(s, "[REDACTED_CREDENTIAL]")
	s = assignments.ReplaceAllString(s, "${1}[REDACTED]")
	s = queryURL.ReplaceAllString(s, "${1}?[REDACTED_QUERY]")
	return binaryData.ReplaceAllString(s, "[OMITTED_BINARY]")
}

func sanitize(v any, depth int, budget *int) any {
	*budget--
	if depth > 64 || *budget < 0 {
		return map[string]any{"omitted": "structure_limit"}
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			if *budget < 0 {
				out["omitted"] = "structure_limit"
				break
			}
			switch {
			case secretKey.MatchString(k):
				out[k] = "[REDACTED]"
			case k == "delta":
				out[k] = map[string]any{"omitted": "incremental_fragment"}
			case k == "encrypted_content" || k == "encryptedContent" || k == "file_data" || k == "image_base64" || k == "b64_json" || k == "audio":
				out[k] = map[string]any{"omitted": "opaque_or_binary"}
			case x["type"] == "reasoning" && (k == "text" || k == "content"):
				out[k] = map[string]any{"omitted": "opaque_reasoning"}
			default:
				if s, ok := item.(string); ok && (k == "arguments" || k == "output" || k == "input") {
					var inner any
					if common.Unmarshal([]byte(s), &inner) == nil {
						b, err := common.Marshal(sanitize(inner, depth+1, budget))
						if err == nil {
							out[k] = string(b)
							continue
						}
					}
				}
				out[k] = sanitize(item, depth+1, budget)
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, item := range x {
			if *budget < 0 {
				out = append(out, map[string]any{"omitted": "structure_limit"})
				break
			}
			out = append(out, sanitize(item, depth+1, budget))
		}
		return out
	case string:
		return clean(x)
	default:
		return v
	}
}

func decode(raw []byte, contentType string) any {
	budget := 200000
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if strings.Contains(contentType, "text/event-stream") && trimmed[0] != '{' && trimmed[0] != '[' {
		events := make([]any, 0)
		for _, frame := range bytes.Split(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), []byte("\n\n")) {
			var data [][]byte
			for _, line := range bytes.Split(frame, []byte("\n")) {
				if bytes.HasPrefix(line, []byte("data:")) {
					data = append(data, bytes.TrimSpace(line[5:]))
				}
			}
			if len(data) == 0 {
				continue
			}
			joined := bytes.Join(data, []byte("\n"))
			if bytes.Equal(joined, []byte("[DONE]")) {
				events = append(events, "[DONE]")
				continue
			}
			var v any
			if common.Unmarshal(joined, &v) != nil {
				events = append(events, map[string]any{"omitted": "invalid_or_partial_sse_json"})
			} else {
				events = append(events, sanitize(v, 0, &budget))
			}
			if budget < 0 {
				events = append(events, map[string]any{"omitted": "structure_limit"})
				break
			}
		}
		return map[string]any{"format": "sse", "events": events}
	}
	var v any
	if common.Unmarshal(raw, &v) != nil {
		return map[string]any{"omitted": "invalid_or_non_json"}
	}
	return map[string]any{"format": "json", "data": sanitize(v, 0, &budget)}
}

func safeHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Encoding", "Accept", "X-Request-Id", "Request-Id", "X-Oneapi-Request-Id", "Cf-Ray", "X-Bridge-Trace-Id", "X-Bridge-Process-Id", "Openai-Processing-Ms", "Retry-After"} {
		if s := h.Get(k); s != "" {
			if len(s) > 512 {
				s = s[:512]
			}
			out[k] = clean(s)
		}
	}
	return out
}
func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
