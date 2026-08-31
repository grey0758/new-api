package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestMemoriesUsesConfiguredOpenAIBasePath(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeMemories,
		RequestURLPath: "/v1/memories/trace_summarize",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: "http://10.253.0.1:18787/outer-tools",
		},
	}

	requestURL, err := (&Adaptor{}).GetRequestURL(info)

	require.NoError(t, err)
	require.Equal(t, "http://10.253.0.1:18787/outer-tools/v1/memories/trace_summarize", requestURL)
}
