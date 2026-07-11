package controller

import (
	"encoding/json"
	"testing"
)

func TestDefaultInstallCommandConfigUsesCurrentModels(t *testing.T) {
	config := defaultInstallCommandConfig()
	wantModels := []string{"gpt-5.5", "gpt-5.6-sol"}
	if len(config.Models) != len(wantModels) {
		t.Fatalf("models = %v, want %v", config.Models, wantModels)
	}
	for index, want := range wantModels {
		if config.Models[index] != want {
			t.Fatalf("models = %v, want %v", config.Models, wantModels)
		}
	}
	if config.DefaultModel != "gpt-5.6-sol" {
		t.Fatalf("default model = %q, want gpt-5.6-sol", config.DefaultModel)
	}
	if len(config.ReasoningEfforts) != 2 || config.ReasoningEfforts[0] != "xhigh" || config.ReasoningEfforts[1] != "max" {
		t.Fatalf("reasoning efforts = %v, want [xhigh max]", config.ReasoningEfforts)
	}
	if config.DefaultReasoningEffort != "xhigh" {
		t.Fatalf("default reasoning effort = %q, want xhigh", config.DefaultReasoningEffort)
	}
}

func TestNormalizeInstallCommandConfigValuePreservesCurrentDefaults(t *testing.T) {
	normalized, err := normalizeInstallCommandConfigValue(`{}`)
	if err != nil {
		t.Fatalf("normalize install command config: %v", err)
	}

	var config installCommandConfig
	if err := json.Unmarshal([]byte(normalized), &config); err != nil {
		t.Fatalf("decode normalized install command config: %v", err)
	}
	if config.DefaultModel != "gpt-5.6-sol" || !containsString(config.Models, "gpt-5.5") || !containsString(config.Models, "gpt-5.6-sol") {
		t.Fatalf("normalized config = %+v", config)
	}
	if config.DefaultReasoningEffort != "xhigh" || !containsString(config.ReasoningEfforts, "xhigh") || !containsString(config.ReasoningEfforts, "max") {
		t.Fatalf("normalized reasoning config = %+v", config)
	}
}
