package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetModelRequestSupportsGeminiCompatAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/v1beta/models/gemini-2.5-flash-lite:generateContent", nil)

	modelRequest, shouldSelectChannel, err := getModelRequest(ctx)
	if err != nil {
		t.Fatalf("getModelRequest returned error: %v", err)
	}
	if !shouldSelectChannel {
		t.Fatalf("shouldSelectChannel = false, want true")
	}
	if modelRequest.Model != "gemini-2.5-flash-lite" {
		t.Fatalf("modelRequest.Model = %q, want %q", modelRequest.Model, "gemini-2.5-flash-lite")
	}
}

func TestExtractModelNameFromGeminiPathSupportsCompatAlias(t *testing.T) {
	got := extractModelNameFromGeminiPath("/v1/v1beta/models/gemini-2.5-pro:countTokens")
	if got != "gemini-2.5-pro" {
		t.Fatalf("extractModelNameFromGeminiPath returned %q, want %q", got, "gemini-2.5-pro")
	}
}
