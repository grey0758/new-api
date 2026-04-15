package constant

import "testing"

func TestIsGeminiPath(t *testing.T) {
	testCases := []struct {
		path string
		want bool
	}{
		{path: "/v1beta/models/gemini-2.5-flash:generateContent", want: true},
		{path: "/v1/v1beta/models/gemini-2.5-flash:generateContent", want: true},
		{path: "/v1beta/openai/models", want: true},
		{path: "/v1/models/foo", want: true},
		{path: "/v1/chat/completions", want: false},
	}

	for _, tc := range testCases {
		if got := IsGeminiPath(tc.path); got != tc.want {
			t.Fatalf("IsGeminiPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestPath2RelayModeSupportsGeminiCompatAlias(t *testing.T) {
	if got := Path2RelayMode("/v1/v1beta/models/gemini-2.5-flash-lite:generateContent"); got != RelayModeGemini {
		t.Fatalf("Path2RelayMode returned %d, want %d", got, RelayModeGemini)
	}
}
