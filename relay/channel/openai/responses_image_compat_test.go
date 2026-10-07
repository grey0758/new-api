package openai

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type channel47ImageRoundTripper func(*http.Request) (*http.Response, error)

func (f channel47ImageRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestChannel47ImageCompatibilityExactChannelAndTransport(t *testing.T) {
	info := krillHistoryInfo()
	info.ChannelId = 47
	require.True(t, usesChannel47ImageCompatibility(info))
	for _, base := range []string{
		"https://api.openai.com/v1",
		"https://api.krill-ai.net.evil.test/codex",
		"https://api.krill-ai.net/v1",
		"https://user@api.krill-ai.net/codex",
		"https://api.krill-ai.net/codex?route=other",
	} {
		info.ChannelBaseUrl = base
		require.False(t, usesChannel47ImageCompatibility(info), base)
	}
	info = krillHistoryInfo()
	info.ChannelId = 46
	body := strings.NewReader(`{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://images.example.test/a.png"}]}]}`)
	prepared, err := PrepareResponsesImageBody(nil, info, body)
	require.NoError(t, err)
	require.Same(t, body, prepared)
	info.ChannelId = 47
	info.RelayMode = relayconstant.RelayModeResponsesCompact
	require.False(t, usesChannel47ImageCompatibility(info))
	require.False(t, usesChannel47ImageCompatibility(nil))
}

func TestChannel47ImageReferencePreservesHistoryAndUnknownFields(t *testing.T) {
	const raw = `{"unknown":9007199254740993,"store":false,"input":[{"role":"user","content":[{"type":"input_text","text":"unchanged"},{"type":"input_image","image_url":"https://images.example.test/a.png","detail":"high","extension":{"n":9007199254740993}}]},{"type":"function_call_output","call_id":"call_original","output":[{"type":"input_text","text":"result"},{"type":"input_image","image_url":"https://images.example.test/a.png"}]},{"type":"custom_tool_call_output","call_id":"call_custom","output":[{"type":"input_image","image_url":"https://images.example.test/b.png"}]}]}`
	const inline = "data:image/png;base64,aW1hZ2U="
	calls := []string{}
	got, err := normalizeChannel47ImageReferences([]byte(raw), func(url string) (string, error) {
		calls = append(calls, url)
		return inline, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"https://images.example.test/a.png", "https://images.example.test/b.png"}, calls)
	want := strings.ReplaceAll(raw, "https://images.example.test/a.png", inline)
	want = strings.ReplaceAll(want, "https://images.example.test/b.png", inline)
	require.Equal(t, want, string(got))
	again, err := normalizeChannel47ImageReferences(got, func(string) (string, error) {
		t.Fatal("an inline image must never be downloaded")
		return "", nil
	})
	require.NoError(t, err)
	require.Equal(t, got, again)
}

func TestChannel47ImageCompatibilityLeavesOtherReferencesAndToolTextIntact(t *testing.T) {
	const raw = `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,YWJj"},{"type":"input_image","file_id":"file-existing"},{"type":"input_image","image_url":"sediment://existing"},{"type":"input_image","image_url":"file:///private.png"},{"type":"input_image","image_url":{"url":"https://images.example.test/a.png"}},{"type":"input_image","image_url":"https://images.example.test/a.png","file_id":"ambiguous"},{"type":"input_file","file_url":"https://files.example.test/a.pdf"}]},{"type":"function_call","arguments":"{\"image_url\":\"https://images.example.test/a.png\"}"},{"type":"function_call_output","output":"https://images.example.test/a.png"}]}`
	got, err := normalizeChannel47ImageReferences([]byte(raw), func(string) (string, error) {
		t.Fatal("opaque references, existing data and unrelated fields must be preserved")
		return "", nil
	})
	require.NoError(t, err)
	require.Equal(t, raw, string(got))
}

func TestChannel47ImageCompatibilityFetchFailureStopsBeforeUpstreamSend(t *testing.T) {
	const raw = `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://images.example.test/signed.png?private=capability"}]}]}`
	got, err := normalizeChannel47ImageReferences([]byte(raw), func(string) (string, error) {
		return "", errors.New("could not download the input image")
	})
	require.Nil(t, got)
	require.EqualError(t, err, "resolve input.0.content.0.image_url: could not download the input image")
	require.NotContains(t, err.Error(), "capability")
	require.NotContains(t, err.Error(), "images.example.test")
}

func TestChannel47ImageCompatibilityRejectsInvalidJSONAndPreservesText(t *testing.T) {
	_, err := normalizeChannel47ImageReferences([]byte(`{"input":`), nil)
	require.Error(t, err)
	info := krillHistoryInfo()
	info.ChannelId = 47
	body, err := PrepareResponsesImageBody(nil, info, strings.NewReader(`{"input":"plain text"}`))
	require.NoError(t, err)
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, `{"input":"plain text"}`, string(got))
}

func TestChannel47ImageDownloadUsesFetchPolicyCacheAndPreservesImageBytes(t *testing.T) {
	service.InitHttpClient()
	client := service.GetHttpClient()
	oldTransport := client.Transport
	oldFetch := *system_setting.GetFetchSetting()
	oldMax := constant.MaxFileDownloadMB
	t.Cleanup(func() {
		client.Transport = oldTransport
		*system_setting.GetFetchSetting() = oldFetch
		constant.MaxFileDownloadMB = oldMax
	})
	constant.MaxFileDownloadMB = 1
	*system_setting.GetFetchSetting() = system_setting.FetchSetting{
		EnableSSRFProtection: true,
		AllowedPorts:         []string{"443"},
	}
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	fetches := 0
	client.Transport = channel47ImageRoundTripper(func(req *http.Request) (*http.Response, error) {
		fetches++
		require.Equal(t, "https://8.8.8.8/test.png", req.URL.String())
		require.Empty(t, req.Header.Get("Authorization"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(imageBytes.Bytes())),
			Request:    req,
		}, nil
	})
	info := krillHistoryInfo()
	info.ChannelId = 47
	info.ApiKey = "synthetic-channel-key-must-not-leak"
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	t.Cleanup(func() { service.CleanupFileSources(c) })
	const raw = `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://8.8.8.8/test.png","detail":"high"}]}]}`
	for i := 0; i < 2; i++ {
		body, err := PrepareResponsesImageBody(c, info, strings.NewReader(raw))
		require.NoError(t, err)
		got, err := io.ReadAll(body)
		require.NoError(t, err)
		inline := gjson.GetBytes(got, "input.0.content.0.image_url").String()
		require.True(t, strings.HasPrefix(inline, "data:image/png;base64,"))
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(inline, "data:image/png;base64,"))
		require.NoError(t, err)
		require.Equal(t, imageBytes.Bytes(), decoded)
		require.Equal(t, "high", gjson.GetBytes(got, "input.0.content.0.detail").String())
	}
	require.Equal(t, 1, fetches, "the same request context should reuse the gateway file cache")
	const privateImage = `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://127.0.0.1/private.png?secret=capability"}]}]}`
	body, err := PrepareResponsesImageBody(c, info, strings.NewReader(privateImage))
	require.Nil(t, body)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "capability")
	require.NotContains(t, err.Error(), "127.0.0.1")
	require.Equal(t, 1, fetches, "the existing SSRF policy must block before any download")
}
