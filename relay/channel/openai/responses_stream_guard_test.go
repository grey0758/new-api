package openai

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestIsResponsesStreamFatalEvent(t *testing.T) {
	tests := []struct {
		name string
		typ  string
		want bool
	}{
		{name: "error", typ: "response.error", want: true},
		{name: "failed", typ: "response.failed", want: true},
		{name: "delta", typ: "response.output_text.delta", want: false},
		{name: "completed", typ: "response.completed", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isResponsesStreamFatalEvent(dto.ResponsesStreamResponse{Type: tt.typ})
			if got != tt.want {
				t.Fatalf("isResponsesStreamFatalEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResponsesStreamFatalEventCyberPolicyIsRequestScoped(t *testing.T) {
	err := responsesStreamFatalEventNewAPIError(dto.ResponsesStreamResponse{
		Type: "response.failed",
		Response: &dto.OpenAIResponsesResponse{
			Error: types.OpenAIError{
				Message: "content rejected",
				Type:    "invalid_request_error",
				Code:    "cyber_policy",
			},
		},
	})

	require.Equal(t, http.StatusForbidden, err.StatusCode)
	require.Equal(t, types.ErrorCode("cyber_policy"), err.GetErrorCode())
	require.True(t, types.IsSkipRetryError(err))
	require.False(t, shouldFailoverResponsesStreamFatalError(err))
}

func TestResponsesStreamFatalEventRateLimitRemainsFailoverEligible(t *testing.T) {
	err := responsesStreamFatalEventNewAPIError(dto.ResponsesStreamResponse{
		Type: "response.failed",
		Response: &dto.OpenAIResponsesResponse{
			Error: types.OpenAIError{
				Message: "retry later",
				Type:    "server_error",
				Code:    "rate_limit_exceeded",
			},
		},
	})

	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.Equal(t, types.ErrorCode("rate_limit_exceeded"), err.GetErrorCode())
	require.False(t, types.IsSkipRetryError(err))
	require.True(t, shouldFailoverResponsesStreamFatalError(err))
}

func TestResponsesStreamFatalEventErrorIncludesUpstreamMessage(t *testing.T) {
	err := responsesStreamFatalEventError(dto.ResponsesStreamResponse{
		Type: "response.failed",
		Response: &dto.OpenAIResponsesResponse{
			Error: types.OpenAIError{
				Message: "upstream exhausted",
				Code:    "insufficient_quota",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "upstream exhausted") {
		t.Fatalf("responsesStreamFatalEventError() = %v, want upstream message", err)
	}
}

func TestShouldCommitResponsesStreamGuard(t *testing.T) {
	tests := []struct {
		name           string
		resp           dto.ResponsesStreamResponse
		bufferedEvents int
		bufferedBytes  int
		want           bool
	}{
		{
			name: "metadata stays buffered",
			resp: dto.ResponsesStreamResponse{Type: "response.created"},
			want: false,
		},
		{
			name: "text delta commits",
			resp: dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "hello"},
			want: true,
		},
		{
			name: "reasoning summary delta stays buffered",
			resp: dto.ResponsesStreamResponse{Type: "response.reasoning_summary_text.delta", Delta: "thinking"},
			want: false,
		},
		{
			name: "reasoning text delta stays buffered",
			resp: dto.ResponsesStreamResponse{Type: "response.reasoning_text.delta", Delta: "thinking"},
			want: false,
		},
		{
			name: "completed commits",
			resp: dto.ResponsesStreamResponse{Type: "response.completed"},
			want: true,
		},
		{
			name:           "event cap commits",
			resp:           dto.ResponsesStreamResponse{Type: "response.in_progress"},
			bufferedEvents: responsesStreamGuardMaxBufferedEvents,
			want:           true,
		},
		{
			name:          "byte cap commits",
			resp:          dto.ResponsesStreamResponse{Type: "response.in_progress"},
			bufferedBytes: responsesStreamGuardMaxBufferedBytes,
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCommitResponsesStreamGuard(tt.resp, tt.bufferedEvents, tt.bufferedBytes); got != tt.want {
				t.Fatalf("shouldCommitResponsesStreamGuard() = %v, want %v", got, tt.want)
			}
		})
	}
}
