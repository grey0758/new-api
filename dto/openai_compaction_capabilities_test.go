package dto

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
)

func TestCodexCompactionCapabilitiesSurviveTypedRoundTrip(t *testing.T) {
	raw := []byte(`{"model":"gpt-6.1-sol","input":[{"type":"function_call_output","call_id":"call_1","output":"ok"}],"tools":[{"type":"function","name":"synthetic"}],"parallel_tool_calls":false,"reasoning":{"context":"all_turns"},"service_tier":"default","prompt_cache_key":"synthetic_cache","prompt_cache_options":{"synthetic":true},"prompt_cache_retention":"24h","text":{"format":{"type":"text"}}}`)
	var request OpenAIResponsesCompactionRequest
	require.NoError(t, common.Unmarshal(raw, &request))
	out, err := common.Marshal(request)
	require.NoError(t, err)
	for _, key := range []string{"input", "tools", "parallel_tool_calls", "reasoning", "service_tier", "prompt_cache_key", "prompt_cache_options", "prompt_cache_retention", "text"} {
		require.JSONEq(t, gjson.GetBytes(raw, key).Raw, gjson.GetBytes(out, key).Raw, key)
	}
}
