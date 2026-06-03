package openai

import (
	"testing"

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
			name: "988665 expects object arguments",
			info: &relaycommon.RelayInfo{UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 33, ChannelBaseUrl: "https://988665.xyz"}},
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
