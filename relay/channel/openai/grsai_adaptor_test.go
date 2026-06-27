package openai

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
)

func TestGrsaiImageGenerationUsesGenerateEndpoint(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeImagesGenerations,
		RequestURLPath: "/v1/images/generations",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    "https://grsaiapi.com",
			UpstreamModelName: "gpt-image-2-vip",
		},
	}

	got, err := (&Adaptor{}).GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL returned error: %v", err)
	}
	if want := "https://grsaiapi.com/v1/api/generate"; got != want {
		t.Fatalf("GetRequestURL() = %q, want %q", got, want)
	}
}

func TestGrsaiNanoBananaImageGenerationUsesGenerateEndpoint(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeImagesGenerations,
		RequestURLPath: "/v1/images/generations",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    "https://grsaiapi.com",
			UpstreamModelName: "nano-banana-2",
		},
	}

	got, err := (&Adaptor{}).GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL returned error: %v", err)
	}
	if want := "https://grsaiapi.com/v1/api/generate"; got != want {
		t.Fatalf("GetRequestURL() = %q, want %q", got, want)
	}
}

func TestGrsaiImageEditUsesGenerateEndpoint(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeImagesEdits,
		RequestURLPath: "/v1/images/edits",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    "https://grsaiapi.com",
			UpstreamModelName: "gpt-image-2-vip",
		},
	}

	got, err := (&Adaptor{}).GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL returned error: %v", err)
	}
	if want := "https://grsaiapi.com/v1/api/generate"; got != want {
		t.Fatalf("GetRequestURL() = %q, want %q", got, want)
	}
}

func TestGrsaiChatModelDoesNotUseGenerateEndpoint(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeImagesEdits,
		RequestURLPath: "/v1/images/edits",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    "https://grsaiapi.com",
			UpstreamModelName: "gemini-3.1-pro",
		},
	}

	got, err := (&Adaptor{}).GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL returned error: %v", err)
	}
	if want := "https://grsaiapi.com/v1/images/edits"; got != want {
		t.Fatalf("GetRequestURL() = %q, want %q", got, want)
	}
}

func TestGrsaiNanoBananaImageGenerationRequestConvertsToNativeBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://grsaiapi.com",
			UpstreamModelName: "nano-banana-2",
		},
	}
	req := dto.ImageRequest{
		Model:          "nano-banana-2",
		Prompt:         "test",
		Size:           "2048x1152",
		ResponseFormat: "url",
	}

	got, err := (&Adaptor{}).ConvertImageRequest(ctx, info, req)
	if err != nil {
		t.Fatalf("ConvertImageRequest returned error: %v", err)
	}

	body, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("ConvertImageRequest returned %T, want map[string]any", got)
	}
	if body["model"] != "nano-banana-2" || body["prompt"] != "test" || body["replyType"] != "json" ||
		body["aspectRatio"] != "16:9" || body["imageSize"] != "2K" {
		t.Fatalf("ConvertImageRequest() = %#v", body)
	}
}

func TestGrsaiNanoBananaProVIPKeepsRatioAndImageSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://grsaiapi.com",
			UpstreamModelName: "nano-banana-pro-4k-vip",
		},
	}
	req := dto.ImageRequest{
		Model:  "nano-banana-pro-4k-vip",
		Prompt: "test",
		Size:   "3840x2160",
	}

	got, err := (&Adaptor{}).ConvertImageRequest(ctx, info, req)
	if err != nil {
		t.Fatalf("ConvertImageRequest returned error: %v", err)
	}

	body, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("ConvertImageRequest returned %T, want map[string]any", got)
	}
	if body["aspectRatio"] != "16:9" || body["imageSize"] != "4K" {
		t.Fatalf("ConvertImageRequest() = %#v", body)
	}
}

func TestGrsaiImageGenerationRequestConvertsToNativeBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://grsaiapi.com",
			UpstreamModelName: "gpt-image-2-vip",
		},
	}
	req := dto.ImageRequest{
		Model:          "gpt-image-2-vip",
		Prompt:         "test",
		Size:           "2048x2048",
		ResponseFormat: "url",
	}

	got, err := (&Adaptor{}).ConvertImageRequest(ctx, info, req)
	if err != nil {
		t.Fatalf("ConvertImageRequest returned error: %v", err)
	}

	body, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("ConvertImageRequest returned %T, want map[string]any", got)
	}
	if body["model"] != "gpt-image-2-vip" || body["prompt"] != "test" || body["replyType"] != "json" || body["aspectRatio"] != "2048x2048" {
		t.Fatalf("ConvertImageRequest() = %#v", body)
	}
}

func TestGrsaiImageGenerationNormalizesInvalidVIP4KSquare(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://grsaiapi.com",
			UpstreamModelName: "gpt-image-2-vip",
		},
	}
	req := dto.ImageRequest{
		Model:  "gpt-image-2-vip",
		Prompt: "test",
		Size:   "4096x4096",
	}

	got, err := (&Adaptor{}).ConvertImageRequest(ctx, info, req)
	if err != nil {
		t.Fatalf("ConvertImageRequest returned error: %v", err)
	}
	body, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("ConvertImageRequest returned %T, want map[string]any", got)
	}
	if body["aspectRatio"] != "2880x2880" {
		t.Fatalf("aspectRatio = %#v, want 2880x2880", body["aspectRatio"])
	}
}

func TestGrsaiStandardImageGenerationUsesRatioFor2KRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://grsaiapi.com",
			UpstreamModelName: "gpt-image-2",
		},
	}
	req := dto.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "test",
		Size:   "2048x2048",
	}

	got, err := (&Adaptor{}).ConvertImageRequest(ctx, info, req)
	if err != nil {
		t.Fatalf("ConvertImageRequest returned error: %v", err)
	}
	body, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("ConvertImageRequest returned %T, want map[string]any", got)
	}
	if body["aspectRatio"] != "1:1" {
		t.Fatalf("aspectRatio = %#v, want 1:1", body["aspectRatio"])
	}
}
