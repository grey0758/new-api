package openai

import (
	"bytes"
	"context"
	"errors"
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
		types.ErrOptionWithPreserveUserError(),
	)
}

// IsGrsaiImageCompat exposes the channel-local GrsAI image compatibility
// predicate to the relay handler so non-2xx native responses can use the same
// provider-specific error parser as successful responses.
func IsGrsaiImageCompat(info *relaycommon.RelayInfo) bool {
	return isGrsaiImageCompat(info)
}

// GrsaiImageErrorHandler preserves provider fields for client-visible 4xx
// responses, while keeping transient 5xx/429 failures on the normal retry
// path and giving native policy responses a stable non-retryable shape.
func GrsaiImageErrorHandler(ctx context.Context, resp *http.Response) *types.NewAPIError {
	if resp == nil || resp.Body == nil {
		return types.NewOpenAIError(io.ErrUnexpectedEOF, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	return newGrsaiImageError(body, resp.StatusCode, ctx)
}

func newGrsaiImageError(body []byte, statusCode int, ctx context.Context) *types.NewAPIError {
	if statusCode < http.StatusBadRequest {
		statusCode = http.StatusServiceUnavailable
	}

	var payload grsaiImageErrorPayload
	if common.Unmarshal(body, &payload) == nil {
		if isGrsaiContentPolicyViolation(payload) {
			return newGrsaiContentPolicyViolationError()
		}
		message := firstNonEmpty(payload.Error.Message, payload.Message, payload.Msg, payload.ErrorMsg, payload.Detail)
		code := firstNonEmpty(payload.Error.Code, payload.Code, payload.Error.Type, payload.Type)
		if message != "" || code != "" || payload.Status != "" {
			if message == "" {
				message = payload.Status
			}
			if code == "" {
				code = string(types.ErrorCodeBadResponseStatusCode)
			}
			return types.WithOpenAIError(types.OpenAIError{
				Message: message,
				Type:    firstNonEmpty(payload.Error.Type, payload.Type, "upstream_error"),
				Code:    code,
			}, statusCode, types.ErrOptionWithPreserveUserError())
		}
	}

	// Reuse the existing generic parser for transient and unstructured
	// failures so their established retry/cooldown behavior is unchanged.
	resp := &http.Response{StatusCode: statusCode, Body: io.NopCloser(bytes.NewReader(body))}
	fallback := service.RelayErrorHandler(ctx, resp, false)
	if fallback == nil {
		fallback = types.NewOpenAIError(errors.New("grsai image request failed"), types.ErrorCodeBadResponseStatusCode, statusCode)
	}
	if fallback != nil && fallback.StatusCode == http.StatusBadRequest && service.IsRequestScopedUpstreamRejectionError(fallback) {
		return newGrsaiContentPolicyViolationError()
	}
	return types.NewError(fallback, fallback.GetErrorCode(), types.ErrOptionWithPreserveUserError())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
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
