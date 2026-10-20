package openai

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
)

func TestLatestCodexReasoningAndToolHistorySurviveConversion(t *testing.T) {
	raw := []byte(`{"model":"gpt-6.1-sol","store":false,"reasoning":{"effort":"medium","summary":"auto","context":"all_turns","mode":{"kind":"synthetic"}},"access_programs":{"synthetic":true},"prompt_cache_options":{"synthetic":true},"input":[{"type":"reasoning","id":"rs_1","encrypted_content":"opaque_synthetic"},{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"custom","input":"{}"},{"type":"custom_tool_call_output","call_id":"call_1","output":"done"}]}`)
	var request dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal(raw, &request))
	copied, err := common.DeepCopy(&request)
	require.NoError(t, err)
	adaptor := Adaptor{}
	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 81, ChannelBaseUrl: "https://api.krill-ai.net/codex"}}, *copied)
	require.NoError(t, err)
	out, err := common.Marshal(converted)
	require.NoError(t, err)
	for _, path := range []string{"reasoning.context", "reasoning.mode", "access_programs", "prompt_cache_options", "input"} {
		require.JSONEq(t, gjson.GetBytes(raw, path).Raw, gjson.GetBytes(out, path).Raw, path)
	}
	var omitted dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-5.6-sol","reasoning":{"effort":"medium"}}`), &omitted))
	out, err = common.Marshal(omitted)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(out, "reasoning.context").Exists())
}
