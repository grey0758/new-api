package openai

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

type grsaiImageErrorPayload struct {
	Status   string `json:"status"`
	Code     string `json:"code"`
	Type     string `json:"type"`
	Message  string `json:"message"`
	Msg      string `json:"msg"`
	ErrorMsg string `json:"error_msg"`
	Detail   string `json:"detail"`
	Error    struct {
		Code    string `json:"code"`
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

const grsaiContentPolicyViolationMessage = "Image request rejected by the provider content policy."

func newGrsaiContentPolicyViolationError() *types.NewAPIError {
	return types.WithOpenAIError(
		types.OpenAIError{
			Message: grsaiContentPolicyViolationMessage,
			Type:    string(types.ErrorCodeContentPolicyViolation),
			Code:    string(types.ErrorCodeContentPolicyViolation),
		},
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}

// IsGrsaiImageCompat exposes the channel-local GrsAI image compatibility
// predicate to the relay handler so non-2xx native responses can use the same
// provider-specific error parser as successful responses.
func IsGrsaiImageCompat(info *relaycommon.RelayInfo) bool {
	return isGrsaiImageCompat(info)
}

// GrsaiImageErrorHandler preserves the normal OpenAI error path for ordinary
// upstream failures, but gives GrsAI's native policy response a stable,
// client-visible, non-retryable 4xx shape.
func GrsaiImageErrorHandler(ctx context.Context, resp *http.Response) *types.NewAPIError {
	if resp == nil || resp.Body == nil {
		return types.NewOpenAIError(
			io.ErrUnexpectedEOF,
			types.ErrorCodeReadResponseBodyFailed,
			http.StatusInternalServerError,
		)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var payload grsaiImageErrorPayload
	if common.Unmarshal(body, &payload) == nil && isGrsaiContentPolicyViolation(payload) {
		return newGrsaiContentPolicyViolationError()
	}

	// Reuse the existing generic parser for non-policy GrsAI failures so
	// transient 5xx/429 and other provider errors keep their old behavior.
	resp.Body = io.NopCloser(bytes.NewReader(body))
	fallback := service.RelayErrorHandler(ctx, resp, false)
	if fallback != nil && fallback.StatusCode == http.StatusBadRequest && service.IsRequestScopedUpstreamRejectionError(fallback) {
		return newGrsaiContentPolicyViolationError()
	}
	return fallback
}

func isGrsaiContentPolicyViolation(payload grsaiImageErrorPayload) bool {
	status := strings.ToLower(strings.TrimSpace(payload.Status))
	if status == "violation" || status == "blocked" || status == "content_policy_violation" {
		return true
	}

	fields := []string{
		payload.Code,
		payload.Type,
		payload.Message,
		payload.Msg,
		payload.ErrorMsg,
		payload.Detail,
		payload.Error.Code,
		payload.Error.Type,
		payload.Error.Message,
	}
	combined := strings.ToLower(strings.Join(fields, " "))
	for _, marker := range []string{
		"content policy violation",
		"content_policy_violation",
		"policy violation",
		"safety violation",
		"content was flagged",
		"may violate our content policies",
		"violate our content policies",
		"prompt blocked",
	} {
		if strings.Contains(combined, marker) {
			return true
		}
	}
	return false
}
