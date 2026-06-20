package openai

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func TestShouldNormalizeResponsesRequestArguments(t *testing.T) {
	tests := []struct {
		name string
		info *relaycommon.RelayInfo
		want bool
	}{
		{
			name: "cliproxy keeps legacy string arguments",
			info: &relaycommon.RelayInfo{UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1, ChannelBaseUrl: "https://cliproxy1.opencodex.uk"}},
			want: false,
		},
		{
			name: "image group keeps legacy string arguments",
			info: &relaycommon.RelayInfo{UsingGroup: "image", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 30, ChannelBaseUrl: "https://cliproxy1.opencodex.uk"}},
			want: false,
		},
		{
			name: "codex2api keeps legacy string arguments",
			info: &relaycommon.RelayInfo{UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 25, ChannelBaseUrl: "https://www.codex2api.com"}},
			want: false,
		},
		{
			name: "local cliproxy keeps legacy string arguments",
			info: &relaycommon.RelayInfo{UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 26, ChannelBaseUrl: "http://127.0.0.1:8317"}},
			want: false,
		},
		{
			name: "988665 expects object arguments",
			info: &relaycommon.RelayInfo{UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 33, ChannelBaseUrl: "https://988665.xyz"}},
			want: true,
		},
		{
			name: "krill responses compatible upstream expects object arguments",
			info: &relaycommon.RelayInfo{UsingGroup: "pro", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 47, ChannelBaseUrl: "https://api.krill-ai.com/codex"}},
			want: true,
		},
		{
			name: "snowbud responses compatible upstream expects object arguments",
			info: &relaycommon.RelayInfo{UsingGroup: "pro", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 45, ChannelBaseUrl: "https://snowbud.xyz"}},
			want: true,
		},
		{
			name: "zz1cc responses compatible upstream expects object arguments",
			info: &relaycommon.RelayInfo{UsingGroup: "pro", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 43, ChannelBaseUrl: "https://zz1cc.cc.cd"}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldNormalizeResponsesRequestArguments(tt.info); got != tt.want {
				t.Fatalf("shouldNormalizeResponsesRequestArguments() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertOpenAIResponsesRequestNormalizesCompatibleUpstreamArguments(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{UsingGroup: "pro", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 47, ChannelBaseUrl: "https://api.krill-ai.com/codex"}}
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: json.RawMessage(`[
			{
				"type":"apply_patch_call",
				"call_id":"call_123",
				"name":"apply_patch",
				"arguments":"{\"cmd\":\"pwd\",\"retry\":false}"
			}
		]`),
	}

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, request)
	if err != nil {
		t.Fatalf("ConvertOpenAIResponsesRequest() error = %v", err)
	}

	responsesRequest, ok := converted.(dto.OpenAIResponsesRequest)
	if !ok {
		t.Fatalf("converted type = %T, want dto.OpenAIResponsesRequest", converted)
	}

	var input []struct {
		Type      string          `json:"type"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(responsesRequest.Input, &input); err != nil {
		t.Fatalf("unmarshal converted input: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("expected one input item, got %d", len(input))
	}

	var arguments map[string]any
	if err := json.Unmarshal(input[0].Arguments, &arguments); err != nil {
		t.Fatalf("arguments should be object: %v; raw=%s", err, input[0].Arguments)
	}
	if got, want := arguments["cmd"], "pwd"; got != want {
		t.Fatalf("cmd = %v, want %v", got, want)
	}
	if got, want := arguments["retry"], false; got != want {
		t.Fatalf("retry = %v, want %v", got, want)
	}
}
