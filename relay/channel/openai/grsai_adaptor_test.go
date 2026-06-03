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

func TestGrsaiImageGenerationKeepsOpenAIEndpoint(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeImagesGenerations,
		RequestURLPath: "/v1/images/generations",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: "https://grsaiapi.com",
		},
	}

	got, err := (&Adaptor{}).GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL returned error: %v", err)
	}
	if want := "https://grsaiapi.com/v1/images/generations"; got != want {
		t.Fatalf("GetRequestURL() = %q, want %q", got, want)
	}
}

func TestGrsaiImageEditUsesGenerateEndpoint(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeImagesEdits,
		RequestURLPath: "/v1/images/edits",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: "https://grsaiapi.com",
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

func TestGrsaiImageGenerationRequestPassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://grsaiapi.com",
		},
	}
	req := dto.ImageRequest{
		Model:          "gpt-image-2",
		Prompt:         "test",
		Size:           "1024x1024",
		ResponseFormat: "url",
	}

	got, err := (&Adaptor{}).ConvertImageRequest(ctx, info, req)
	if err != nil {
		t.Fatalf("ConvertImageRequest returned error: %v", err)
	}

	passthrough, ok := got.(dto.ImageRequest)
	if !ok {
		t.Fatalf("ConvertImageRequest returned %T, want dto.ImageRequest", got)
	}
	if passthrough.Model != req.Model || passthrough.Prompt != req.Prompt || passthrough.Size != req.Size || passthrough.ResponseFormat != req.ResponseFormat {
		t.Fatalf("ConvertImageRequest() = %#v, want %#v", passthrough, req)
	}
}
