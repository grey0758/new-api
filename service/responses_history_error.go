package service

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/types"
)

const ResponsesHistoryIDErrorCode types.ErrorCode = "invalid_id_prefix"

// Match the provider's parameter diagnostic, not arbitrary mentions in text.
var responsesHistoryIDDiagnostic = regexp.MustCompile(`(?i)\[ApiIdParam\]\s*\[input\[[0-9]+\]\.id\]\s*\[invalid_id_prefix\]`)

func IsResponsesHistoryIDError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if err.GetErrorCode() == ResponsesHistoryIDErrorCode {
		return true
	}
	if upstream, ok := err.RelayError.(types.OpenAIError); ok {
		if code, ok := upstream.Code.(string); ok && strings.EqualFold(code, string(ResponsesHistoryIDErrorCode)) {
			return true
		}
		if responsesHistoryIDDiagnostic.MatchString(upstream.Message) {
			return true
		}
	}
	return responsesHistoryIDDiagnostic.MatchString(err.Error())
}

// A deterministic history validation rejection cannot be repaired by retrying
// another channel. Do not rewrite item IDs or replay tools to make it pass.
func NormalizeResponsesHistoryIDError(err *types.NewAPIError) *types.NewAPIError {
	if !IsResponsesHistoryIDError(err) {
		return err
	}
	return types.WithOpenAIError(types.OpenAIError{
		Type:    "invalid_request_error",
		Code:    string(ResponsesHistoryIDErrorCode),
		Message: "Conversation history item ID format is incompatible with the selected provider (invalid_id_prefix). Resume through the original compatible route with a new complete user turn. Do not resend tool outputs or repeat completed operations.",
	}, http.StatusBadRequest, types.ErrOptionWithSkipRetry(), types.ErrOptionWithPreserveUserError())
}
