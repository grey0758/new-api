package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestNormalizeChannelTestEndpointUsesResponsesForOuterToolsBase(t *testing.T) {
	for _, rawBaseURL := range []string{
		"http://10.253.0.1:18789/outer-tools",
		" http://10.253.0.1:18789/outer-tools/ ",
	} {
		baseURL := rawBaseURL
		channel := &model.Channel{
			Type:    constant.ChannelTypeOpenAI,
			BaseURL: &baseURL,
		}
		require.Equal(
			t,
			string(constant.EndpointTypeOpenAIResponse),
			normalizeChannelTestEndpoint(channel, "gpt-5.6-sol", ""),
		)
	}
}

func TestNormalizeChannelTestEndpointKeepsExplicitEndpoint(t *testing.T) {
	baseURL := "http://10.253.0.1:18789/outer-tools"
	channel := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		BaseURL: &baseURL,
	}
	require.Equal(
		t,
		string(constant.EndpointTypeOpenAI),
		normalizeChannelTestEndpoint(
			channel, "gpt-5.6-sol", string(constant.EndpointTypeOpenAI),
		),
	)
}

func TestNormalizeChannelTestEndpointKeepsOrdinaryOpenAIDefault(t *testing.T) {
	baseURL := "https://api.example.invalid"
	channel := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		BaseURL: &baseURL,
	}
	require.Empty(
		t,
		normalizeChannelTestEndpoint(channel, "gpt-5.6-sol", ""),
	)
}
