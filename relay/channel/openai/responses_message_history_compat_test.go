package openai

import (
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const genericMessageHistoryFixture = `{"model":"gpt-6.1-sol","store":false,"stream":true,"reasoning":{"effort":"xhigh","context":"all_turns"},"future":{"n":9007199254740993},"input":[{"type":"message","id":"item_generic_0","status":"completed","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Keep item_generic_0 and call_original verbatim.","annotations":[],"logprobs":[]}],"future_field":{"id":"item_generic_0"}},{"type":"custom_tool_call","id":"ctc_original","call_id":"call_original","name":"exec","input":"already executed"},{"type":"custom_tool_call_output","id":"ctco_original","call_id":"call_original","output":"completed result"},{"role":"user","content":"continue remaining work"}]}`

func TestKrillGenericMessageHistoryConvertedAndPassthrough(t *testing.T) {
	for _, passthrough := range []bool{true, false} {
		t.Run(map[bool]string{true: "passthrough", false: "converted"}[passthrough], func(t *testing.T) {
			raw := []byte(genericMessageHistoryFixture)
			if !passthrough {
				var request dto.OpenAIResponsesRequest
				require.NoError(t, common.Unmarshal(raw, &request))
				converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, krillHistoryInfo(), request)
				require.NoError(t, err)
				raw, err = common.Marshal(converted)
				require.NoError(t, err)
			}
			original := append([]byte(nil), raw...)
			prepared, err := PrepareResponsesHistoryBody(krillHistoryInfo(), strings.NewReader(string(raw)))
			require.NoError(t, err)
			got, err := io.ReadAll(prepared)
			require.NoError(t, err)
			expected, err := sjson.DeleteBytes(raw, "input.0.id")
			require.NoError(t, err)
			require.Equal(t, expected, got)
			require.Equal(t, original, raw, "caller history must remain intact")
			require.Equal(t, gjson.GetBytes(raw, "input.0.content").Raw, gjson.GetBytes(got, "input.0.content").Raw)
			require.Equal(t, gjson.GetBytes(raw, "input.1").Raw, gjson.GetBytes(got, "input.1").Raw)
			require.Equal(t, gjson.GetBytes(raw, "input.2").Raw, gjson.GetBytes(got, "input.2").Raw)
			again, err := normalizeKrillCustomToolHistory(got)
			require.NoError(t, err)
			require.Equal(t, got, again)
		})
	}
}

func TestKrillGenericMessageHistoryRetainsLinkedAmbiguousAndIncompleteItems(t *testing.T) {
	cases := map[string]string{
		"stored":          strings.Replace(genericMessageHistoryFixture, `"store":false`, `"store":true`, 1),
		"store_absent":    strings.Replace(genericMessageHistoryFixture, `"store":false,`, "", 1),
		"previous":        strings.Replace(genericMessageHistoryFixture, `"store":false`, `"store":false,"previous_response_id":"resp_previous"`, 1),
		"conversation":    strings.Replace(genericMessageHistoryFixture, `"store":false`, `"store":false,"conversation":{"id":"conv_previous"}`, 1),
		"reference":       strings.Replace(genericMessageHistoryFixture, `{"role":"user","content":"continue remaining work"}`, `{"type":"item_reference","id":"item_generic_0"}`, 1),
		"duplicate":       strings.Replace(genericMessageHistoryFixture, `{"role":"user","content":"continue remaining work"}`, `{"type":"message","id":"item_generic_0","role":"assistant","status":"completed","content":[{"type":"output_text","text":"second"}]}`, 1),
		"valid_id":        strings.Replace(genericMessageHistoryFixture, `"id":"item_generic_0"`, `"id":"msg_original"`, 1),
		"unknown_id":      strings.Replace(genericMessageHistoryFixture, `"id":"item_generic_0"`, `"id":"foreign_original"`, 1),
		"user":            strings.Replace(genericMessageHistoryFixture, `"role":"assistant"`, `"role":"user"`, 1),
		"incomplete":      strings.Replace(genericMessageHistoryFixture, `"status":"completed"`, `"status":"incomplete"`, 1),
		"status_absent":   strings.Replace(genericMessageHistoryFixture, `"status":"completed",`, "", 1),
		"missing_text":    strings.Replace(genericMessageHistoryFixture, `"text":"Keep item_generic_0 and call_original verbatim.",`, "", 1),
		"unknown_content": strings.Replace(genericMessageHistoryFixture, `"type":"output_text"`, `"type":"future_content"`, 1),
		"not_message":     strings.Replace(genericMessageHistoryFixture, `"type":"message"`, `"type":"reasoning"`, 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := normalizeKrillCustomToolHistory([]byte(raw))
			require.NoError(t, err)
			require.Equal(t, raw, string(got))
		})
	}
}

func TestKrillGenericMessageHistoryLeavesOtherProvidersUntouched(t *testing.T) {
	info := krillHistoryInfo()
	info.ChannelBaseUrl = "http://10.253.0.1:18787/outer-tools"
	reader := strings.NewReader(genericMessageHistoryFixture)
	got, err := PrepareResponsesHistoryBody(info, reader)
	require.NoError(t, err)
	require.Same(t, reader, got)
}
