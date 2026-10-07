package openai

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// NewResponsesInputImageError preserves a sanitized local client failure
// without recording it as an upstream channel health failure.
func NewResponsesInputImageError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeConvertRequestFailed, http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog(), types.ErrOptionWithPreserveUserError())
}

// PrepareResponsesImageBody resolves downloadable image references before the
// channel47 Krill Codex transport sees them. Its rustponsesapi backend requires
// inline images and cannot resolve assets itself. File IDs and opaque resource
// pointers cannot be resolved by this gateway and are never invented or dropped.
// Apply after overrides and passthrough selection, before the first send.
func PrepareResponsesImageBody(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (io.Reader, error) {
	if !usesChannel47ImageCompatibility(info) {
		return body, nil
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read Responses image input: %w", err)
	}
	normalized, err := normalizeChannel47ImageReferences(raw, func(imageURL string) (string, error) {
		// Reuse the gateway's fetch policy, download limit and request-scoped
		// file cache. Never send the channel API key to an image host.
		data, mimeType, err := service.GetBase64Data(c, types.NewURLFileSource(imageURL), "channel47_inline_image")
		if err != nil {
			// Fetch errors can contain signed URLs. Keep them out of public
			// errors and channel lifecycle logs.
			return "", fmt.Errorf("could not download the input image")
		}
		switch strings.ToLower(mimeType) {
		case "image/png", "image/jpeg", "image/webp", "image/gif":
		default:
			return "", fmt.Errorf("input image has an unsupported media type")
		}
		return "data:" + mimeType + ";base64," + data, nil
	})
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(normalized), nil
}

func usesChannel47ImageCompatibility(info *relaycommon.RelayInfo) bool {
	return usesKrillCodexHistoryCompatibility(info) && info.ChannelId == 47
}

func normalizeChannel47ImageReferences(raw []byte, resolve func(string) (string, error)) ([]byte, error) {
	if !gjson.ValidBytes(raw) {
		return nil, fmt.Errorf("invalid Responses JSON body")
	}
	root := gjson.ParseBytes(raw)
	input := root.Get("input")
	if !root.IsObject() || !input.IsArray() {
		return raw, nil
	}
	normalized := raw
	resolved := make(map[string]string)
	for i, item := range input.Array() {
		field := "content"
		switch item.Get("type").String() {
		case "", "message":
			if item.Get("role").Type != gjson.String {
				continue
			}
		case "function_call_output", "custom_tool_call_output":
			field = "output"
		default:
			continue
		}
		parts := item.Get(field)
		if !parts.IsArray() {
			continue
		}
		for j, part := range parts.Array() {
			imageURL := part.Get("image_url")
			if part.Get("type").String() != "input_image" || imageURL.Type != gjson.String || part.Get("file_id").Exists() {
				continue
			}
			u, err := url.Parse(imageURL.Str)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
				continue
			}
			path := "input." + strconv.Itoa(i) + "." + field + "." + strconv.Itoa(j) + ".image_url"
			inline, ok := resolved[imageURL.Str]
			if !ok {
				inline, err = resolve(imageURL.Str)
				if err != nil {
					return nil, fmt.Errorf("resolve %s: %w", path, err)
				}
				resolved[imageURL.Str] = inline
			}
			normalized, err = sjson.SetBytes(normalized, path, inline)
			if err != nil {
				return nil, fmt.Errorf("normalize input image URL: %w", err)
			}
		}
	}
	return normalized, nil
}
