package openai

import (
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const customHistoryFixture = `{"model":"gpt-5.6-sol","store":false,"stream":true,"future_control":{"n":9007199254740993},"input":[{"type":"additional_tools","tools":[{"type":"custom","name":"exec"}]},{"type":"custom_tool_call","id":"fc_old","call_id":"call_original","name":"exec","input":"{\"id\":\"fc_old\",\"n\":9007199254740993}","status":"completed"},{"type":"custom_tool_call_output","call_id":"call_original","output":[{"type":"input_text","text":"unchanged output fc_old"}]},{"role":"user","content":"continue"}]}`

func krillHistoryInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api.krill-ai.net/codex"}}
}

func TestKrillCustomHistoryPreservesEverythingExceptOptionalItemID(t *testing.T) {
	got, err := normalizeKrillCustomToolHistory([]byte(customHistoryFixture))
	require.NoError(t, err)
	expected, err := sjson.Delete(customHistoryFixture, "input.1.id")
	require.NoError(t, err)
	require.Equal(t, expected, string(got))
	require.Equal(t, "call_original", gjson.GetBytes(got, "input.1.call_id").String())
	require.Equal(t, "call_original", gjson.GetBytes(got, "input.2.call_id").String())
	again, err := normalizeKrillCustomToolHistory(got)
	require.NoError(t, err)
	require.Equal(t, got, again)
}

func TestKrillCustomHistoryConvertedAndPassthroughBodies(t *testing.T) {
	for _, passthrough := range []bool{true, false} {
		t.Run(map[bool]string{true: "passthrough", false: "converted"}[passthrough], func(t *testing.T) {
			raw := []byte(customHistoryFixture)
			if !passthrough {
				var request dto.OpenAIResponsesRequest
				require.NoError(t, common.Unmarshal(raw, &request))
				converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, krillHistoryInfo(), request)
				require.NoError(t, err)
				raw, err = common.Marshal(converted)
				require.NoError(t, err)
			}
			prepared, err := PrepareResponsesHistoryBody(krillHistoryInfo(), strings.NewReader(string(raw)))
			require.NoError(t, err)
			got, err := io.ReadAll(prepared)
			require.NoError(t, err)
			expected, err := sjson.DeleteBytes(raw, "input.1.id")
			require.NoError(t, err)
			require.Equal(t, expected, got)
			require.False(t, gjson.GetBytes(got, "input.1.id").Exists())
		})
	}
}

func TestKrillCustomHistoryOnlyExactProviderAndResponses(t *testing.T) {
	for _, base := range []string{"https://api.openai.com/v1", "http://10.253.0.1:18787/outer-tools", "https://api.krill-ai.net/v1", "https://api.krill-ai.net.evil.test/codex", "https://evil.test/api.krill-ai.net/codex", "https://api.krill-ai.net/codex?x=1", "https://user@api.krill-ai.net/codex"} {
		t.Run(base, func(t *testing.T) {
			info := krillHistoryInfo()
			info.ChannelBaseUrl = base
			reader := strings.NewReader(customHistoryFixture)
			got, err := PrepareResponsesHistoryBody(info, reader)
			require.NoError(t, err)
			require.Same(t, reader, got)
		})
	}
	info := krillHistoryInfo()
	info.RelayMode = relayconstant.RelayModeResponsesCompact
	reader := strings.NewReader(customHistoryFixture)
	got, err := PrepareResponsesHistoryBody(info, reader)
	require.NoError(t, err)
	require.Same(t, reader, got)
}

func TestKrillCustomHistoryDoesNotChangeLinkedOrAmbiguousRecords(t *testing.T) {
	cases := map[string]string{
		"store_true":        strings.Replace(customHistoryFixture, `"store":false`, `"store":true`, 1),
		"store_absent":      strings.Replace(customHistoryFixture, `"store":false,`, "", 1),
		"previous_response": strings.Replace(customHistoryFixture, `"store":false`, `"store":false,"previous_response_id":"resp_old"`, 1),
		"conversation":      strings.Replace(customHistoryFixture, `"store":false`, `"store":false,"conversation":"conv_old"`, 1),
		"correct_custom_id": strings.Replace(customHistoryFixture, `"id":"fc_old"`, `"id":"ctc_old"`, 1),
		"function_call":     strings.Replace(customHistoryFixture, `"type":"custom_tool_call"`, `"type":"function_call"`, 1),
		"unknown_prefix":    strings.Replace(customHistoryFixture, `"id":"fc_old"`, `"id":"other_old"`, 1),
		"missing_call_id":   strings.Replace(customHistoryFixture, `"call_id":"call_original",`, "", 1),
		"reference":         strings.Replace(customHistoryFixture, `{"role":"user","content":"continue"}`, `{"type":"item_reference","id":"fc_old"}`, 1),
		"duplicate_id":      strings.Replace(customHistoryFixture, `{"role":"user","content":"continue"}`, `{"type":"custom_tool_call","id":"fc_old","call_id":"other","name":"exec","input":"other"}`, 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := normalizeKrillCustomToolHistory([]byte(raw))
			require.NoError(t, err)
			require.Equal(t, raw, string(got))
		})
	}
}

func TestKrillCustomOutputLegacyIDPreservesResultAndCorrelation(t *testing.T) {
	for _, output := range []string{`"completed result"`, `[{"type":"input_text","text":"completed result"},{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]`} {
		raw := []byte(`{"store":false,"input":[{"type":"custom_tool_call","id":"fc_old","call_id":"call_original","name":"exec","input":"historical"},{"type":"custom_tool_call_output","id":"fco_old","call_id":"call_original","output":` + output + `}]}`)
		got, err := normalizeKrillCustomToolHistory(raw)
		require.NoError(t, err)
		expected, err := sjson.DeleteBytes(raw, "input.0.id")
		require.NoError(t, err)
		expected, err = sjson.DeleteBytes(expected, "input.1.id")
		require.NoError(t, err)
		require.Equal(t, expected, got)
		require.Equal(t, output, gjson.GetBytes(got, "input.1.output").Raw)
		require.Equal(t, "call_original", gjson.GetBytes(got, "input.1.call_id").String())
	}
}

func TestKrillCustomOutputLeavesUnsupportedOrReferencedRecords(t *testing.T) {
	for name, item := range map[string]string{
		"correct":        `{"type":"custom_tool_call_output","id":"ctco_old","call_id":"call_original","output":"done"}`,
		"function":       `{"type":"function_call_output","id":"fco_old","call_id":"call_original","output":"done"}`,
		"missing_call":   `{"type":"custom_tool_call_output","id":"fco_old","output":"done"}`,
		"missing_output": `{"type":"custom_tool_call_output","id":"fco_old","call_id":"call_original"}`,
		"invalid_output": `{"type":"custom_tool_call_output","id":"fco_old","call_id":"call_original","output":false}`,
		"referenced":     `{"type":"custom_tool_call_output","id":"fco_old","call_id":"call_original","output":"done"},{"type":"item_reference","id":"fco_old"}`,
	} {
		t.Run(name, func(t *testing.T) {
			raw := []byte(`{"store":false,"input":[` + item + `]}`)
			got, err := normalizeKrillCustomToolHistory(raw)
			require.NoError(t, err)
			require.Equal(t, raw, got)
		})
	}
}
