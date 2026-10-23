package openai

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// PrepareResponsesHistoryBody handles the Krill Codex validator's rejection of
// legacy fc_/fco_ custom-tool IDs and generic item_ assistant-message
// IDs. Self-contained assistant inputs do not require a persisted item ID;
// call_id remains the tool-result correlation identity.
// Only the outbound, explicitly stateless request is changed. Never rewrite
// call_id, outputs, stored-response references, or the caller's retained history.
// Run after both conversion and passthrough selection, before the first send.
func PrepareResponsesHistoryBody(info *relaycommon.RelayInfo, body io.Reader) (io.Reader, error) {
	if !usesKrillCodexHistoryCompatibility(info) {
		return body, nil
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeKrillCustomToolHistory(raw)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(normalized), nil
}

func usesKrillCodexHistoryCompatibility(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil || info.RelayMode != relayconstant.RelayModeResponses {
		return false
	}
	u, err := url.Parse(info.ChannelBaseUrl)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "api.krill-ai.net" || host == "krill-ai.net") && strings.TrimRight(u.Path, "/") == "/codex"
}

func normalizeKrillCustomToolHistory(raw []byte) ([]byte, error) {
	if !gjson.ValidBytes(raw) {
		return nil, fmt.Errorf("invalid Responses JSON body")
	}
	root := gjson.ParseBytes(raw)
	if !root.IsObject() || root.Get("store").Type != gjson.False {
		return raw, nil
	}
	// A linked/stored conversation can reference the item ID. Do not alter it.
	for _, key := range []string{"previous_response_id", "conversation"} {
		value := root.Get(key)
		if value.Exists() && value.Type != gjson.Null {
			return raw, nil
		}
	}
	input := root.Get("input")
	if !input.IsArray() {
		return raw, nil
	}
	items := input.Array()
	references := map[string]bool{}
	counts := map[string]int{}
	for _, item := range items {
		id := item.Get("id").String()
		if id != "" {
			counts[id]++
		}
		if item.Get("type").String() == "item_reference" {
			references[id] = true
		}
	}
	normalized := raw
	for index, item := range items {
		id := item.Get("id")
		kind := item.Get("type").String()
		legacyPrefix := ""
		switch kind {
		case "custom_tool_call":
			legacyPrefix = "fc_"
		case "custom_tool_call_output":
			legacyPrefix = "fco_"
		case "message":
			if !isCompleteAssistantHistoryMessage(item) {
				continue
			}
			legacyPrefix = "item_"
		default:
			continue
		}
		if id.Type != gjson.String || !strings.HasPrefix(id.Str, legacyPrefix) || len(id.Str) <= len(legacyPrefix) {
			continue
		}
		// Leave malformed/ambiguous records for normal upstream validation.
		if counts[id.Str] != 1 || references[id.Str] {
			continue
		}
		if kind != "message" && (item.Get("call_id").Type != gjson.String || item.Get("call_id").Str == "") {
			continue
		}
		if kind == "custom_tool_call" && (item.Get("name").Type != gjson.String || item.Get("name").Str == "" || item.Get("input").Type != gjson.String) {
			continue
		}
		if kind == "custom_tool_call_output" && item.Get("output").Type != gjson.String && !item.Get("output").IsArray() {
			continue
		}
		var err error
		normalized, err = sjson.DeleteBytes(normalized, "input."+strconv.Itoa(index)+".id")
		if err != nil {
			return nil, fmt.Errorf("normalize optional history item ID: %w", err)
		}
	}
	return normalized, nil
}

// Only a complete, self-contained assistant message is safe to resend as input
// without an item ID. Keep partial, referenced, malformed and unknown records
// unchanged for the provider to validate. Content/phase/annotations are retained.
func isCompleteAssistantHistoryMessage(item gjson.Result) bool {
	if item.Get("role").String() != "assistant" || item.Get("status").String() != "completed" {
		return false
	}
	content := item.Get("content")
	if !content.IsArray() || len(content.Array()) == 0 {
		return false
	}
	for _, part := range content.Array() {
		switch part.Get("type").String() {
		case "output_text":
			if part.Get("text").Type != gjson.String {
				return false
			}
		case "refusal":
			if part.Get("refusal").Type != gjson.String {
				return false
			}
		default:
			return false
		}
	}
	return true
}
