package dto

import (
	"encoding/json"
	"testing"
)

func TestResolveGeminiImagePriceRatio(t *testing.T) {
	testCases := []struct {
		name      string
		modelName string
		imageSize string
		want      float64
	}{
		{
			name:      "gemini 3.1 flash 2k",
			modelName: "gemini-3.1-flash-image-preview",
			imageSize: "2K",
			want:      gemini31FlashImagePrice2K / gemini31FlashImagePrice1K,
		},
		{
			name:      "gemini 3 pro 4k",
			modelName: "gemini-3-pro-image-preview",
			imageSize: "4K",
			want:      gemini3ProImagePrice4K / gemini3ProImagePrice1K,
		},
		{
			name:      "banana flash alias 2k",
			modelName: "nano-banana-c-2-2k-apipudding",
			imageSize: "",
			want:      gemini31FlashImagePrice2K / gemini31FlashImagePrice1K,
		},
		{
			name:      "banana pro large alias",
			modelName: "nano-banana-c-pro-large-apipudding",
			imageSize: "",
			want:      gemini3ProImagePrice4K / gemini3ProImagePrice1K,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &ImageRequest{
				Model: tc.modelName,
			}
			size := tc.imageSize
			if size == "" {
				size = resolveGeminiImageSize(req)
			}
			got := resolveGeminiImagePriceRatio(tc.modelName, size)
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveGeminiImageSizeFromExtraBody(t *testing.T) {
	req := &ImageRequest{
		Model: "gemini-3.1-flash-image-preview",
		Extra: map[string]json.RawMessage{
			"extra_body": json.RawMessage(`{"google":{"image_config":{"image_size":"4K"}}}`),
		},
	}

	got := resolveGeminiImageSize(req)
	if got != "4K" {
		t.Fatalf("got %q, want %q", got, "4K")
	}

	ratio := resolveGeminiImagePriceRatio(req.Model, got)
	want := gemini31FlashImagePrice4K / gemini31FlashImagePrice1K
	if ratio != want {
		t.Fatalf("got ratio %v, want %v", ratio, want)
	}
}
