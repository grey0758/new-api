package dto

import (
	"encoding/json"
	"testing"
)

func TestResponsesOutputUnmarshalArgumentsObject(t *testing.T) {
	payload := []byte(`{
		"type":"response.output_item.done",
		"item":{
			"type":"function_call",
			"id":"fc_123",
			"call_id":"call_123",
			"name":"run_shell",
			"arguments":{"cmd":"pwd","retry":false}
		}
	}`)

	var response ResponsesStreamResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("unmarshal stream response: %v", err)
	}

	if response.Item == nil {
		t.Fatal("expected item")
	}
	if got, want := response.Item.Arguments, `{"cmd":"pwd","retry":false}`; got != want {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
}

func TestResponsesOutputUnmarshalNestedResponseArgumentsObject(t *testing.T) {
	payload := []byte(`{
		"type":"response.completed",
		"response":{
			"id":"resp_123",
			"output":[{
				"type":"function_call",
				"id":"fc_123",
				"call_id":"call_123",
				"name":"run_shell",
				"arguments":{"cmd":"pwd","retry":false}
			}]
		}
	}`)

	var response ResponsesStreamResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("unmarshal stream response: %v", err)
	}

	if response.Response == nil || len(response.Response.Output) != 1 {
		t.Fatalf("expected one response output, got %#v", response.Response)
	}
	if got, want := response.Response.Output[0].Arguments, `{"cmd":"pwd","retry":false}`; got != want {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
}

func TestResponsesOutputUnmarshalArgumentsString(t *testing.T) {
	payload := []byte(`{
		"type":"function_call",
		"id":"fc_123",
		"arguments":"{\"cmd\":\"pwd\"}"
	}`)

	var output ResponsesOutput
	if err := json.Unmarshal(payload, &output); err != nil {
		t.Fatalf("unmarshal response output: %v", err)
	}

	if got, want := output.Arguments, `{"cmd":"pwd"}`; got != want {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
}

func TestNormalizeResponsesStreamArgumentsData(t *testing.T) {
	payload := `{
		"type":"response.completed",
		"response":{
			"id":"resp_123",
			"output":[{
				"type":"function_call",
				"id":"fc_123",
				"arguments":{"cmd":"pwd","retry":false}
			}]
		}
	}`

	normalized := NormalizeResponsesStreamArgumentsData(payload)

	var response ResponsesStreamResponse
	if err := json.Unmarshal([]byte(normalized), &response); err != nil {
		t.Fatalf("unmarshal normalized stream response: %v", err)
	}
	if response.Response == nil || len(response.Response.Output) != 1 {
		t.Fatalf("expected one response output, got %#v", response.Response)
	}
	if got, want := response.Response.Output[0].Arguments, `{"cmd":"pwd","retry":false}`; got != want {
		t.Fatalf("arguments = %q, want %q", got, want)
	}

	var raw struct {
		Response struct {
			Output []struct {
				Arguments string `json:"arguments"`
			} `json:"output"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(normalized), &raw); err != nil {
		t.Fatalf("unmarshal raw normalized stream response: %v", err)
	}
	if got, want := raw.Response.Output[0].Arguments, `{"cmd":"pwd","retry":false}`; got != want {
		t.Fatalf("raw arguments = %q, want %q", got, want)
	}
}

func TestNormalizeResponsesRequestInputArguments(t *testing.T) {
	input := json.RawMessage(`[
		{
			"type":"apply_patch_call",
			"call_id":"call_123",
			"name":"run_shell",
			"arguments":"{\"cmd\":\"pwd\",\"retry\":false}"
		},
		{
			"type":"message",
			"role":"user",
			"content":"hello"
		}
	]`)

	normalized := NormalizeResponsesRequestInputArguments(input)

	var raw []struct {
		Type      string          `json:"type"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(normalized, &raw); err != nil {
		t.Fatalf("unmarshal normalized request input: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("expected two input items, got %d", len(raw))
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw[0].Arguments, &arguments); err != nil {
		t.Fatalf("arguments should be object: %v; raw=%s", err, raw[0].Arguments)
	}
	if got, want := arguments["cmd"], "pwd"; got != want {
		t.Fatalf("cmd = %v, want %v", got, want)
	}
	if got, want := arguments["retry"], false; got != want {
		t.Fatalf("retry = %v, want %v", got, want)
	}
}

func TestNormalizeResponsesRequestInputArgumentsLeavesNonObjectString(t *testing.T) {
	input := json.RawMessage(`[{"type":"function_call","arguments":"not-json"}]`)
	normalized := NormalizeResponsesRequestInputArguments(input)
	if string(normalized) != string(input) {
		t.Fatalf("normalized = %s, want unchanged %s", normalized, input)
	}
}

func TestNormalizeResponsesRequestInputArgumentsKeepsFunctionCallString(t *testing.T) {
	input := json.RawMessage(`[{"type":"function_call","arguments":"{\"cmd\":\"pwd\"}"}]`)
	normalized := NormalizeResponsesRequestInputArguments(input)
	if string(normalized) != string(input) {
		t.Fatalf("normalized = %s, want unchanged %s", normalized, input)
	}
}
