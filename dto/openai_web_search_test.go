package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWebSearchRequestPreservesExplicitZeroMaxOutputTokens(t *testing.T) {
	zero := uint64(0)
	request := OpenAIWebSearchRequest{
		ID:              "search-1",
		Model:           "gpt-5.6-sol",
		Commands:        []byte(`{"search_query":[{"q":"codex"}]}`),
		MaxOutputTokens: &zero,
	}

	body, err := common.Marshal(request)
	require.NoError(t, err)
	require.Contains(t, string(body), `"max_output_tokens":0`)
}

func TestOpenAIWebSearchRequestTokenMetaIncludesProtocolPayload(t *testing.T) {
	request := OpenAIWebSearchRequest{
		Input:    []byte(`"context-marker"`),
		Commands: []byte(`{"search_query":[{"q":"query-marker"}]}`),
		Settings: []byte(`{"search_context_size":"low"}`),
	}

	meta := request.GetTokenCountMeta()
	require.Contains(t, meta.CombineText, "context-marker")
	require.Contains(t, meta.CombineText, "query-marker")
	require.Contains(t, meta.CombineText, "search_context_size")
}
