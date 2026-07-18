package ratio_setting

import "testing"

func TestGPT56CompletionRatio(t *testing.T) {
	models := []string{
		"gpt-5.6-luna",
		"gpt-5.6-sol",
		"gpt-5.6-terra",
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			if got := GetCompletionRatio(model); got != 6 {
				t.Fatalf("GetCompletionRatio(%q) = %v, want 6", model, got)
			}
		})
	}
}
