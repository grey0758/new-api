package openai

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
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
