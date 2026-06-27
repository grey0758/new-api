package controller

import (
	"encoding/json"
	"testing"
)

func TestRuntimeHeaderNavModulesInstallEnvOverrides(t *testing.T) {
	t.Setenv("NEWAPI_INSTALL_ENABLED", "false")
	t.Setenv("NEWAPI_INSTALL_LINK", "https://apicc.opencodex.uk/claude-install/")

	got := runtimeHeaderNavModules(`{"home":true,"install":true}`, "https://example.com/install/")

	var modules map[string]interface{}
	if err := json.Unmarshal([]byte(got), &modules); err != nil {
		t.Fatalf("runtimeHeaderNavModules returned invalid JSON: %v", err)
	}
	install, ok := modules["install"].(map[string]interface{})
	if !ok {
		t.Fatalf("install config missing or wrong type: %#v", modules["install"])
	}
	if install["enabled"] != false {
		t.Fatalf("install enabled = %#v, want false", install["enabled"])
	}
	if install["link"] != "https://apicc.opencodex.uk/claude-install/" {
		t.Fatalf("install link = %#v", install["link"])
	}
	if modules["home"] != true {
		t.Fatalf("home module was not preserved: %#v", modules["home"])
	}
}

func TestRuntimeHeaderNavModulesFullEnvOverride(t *testing.T) {
	t.Setenv("NEWAPI_HEADER_NAV_MODULES", `{"install":{"enabled":true,"link":"/claude-install/"}}`)

	got := runtimeHeaderNavModules(`{"install":false}`, "https://example.com/install/")
	if got != `{"install":{"enabled":true,"link":"/claude-install/"}}` {
		t.Fatalf("runtimeHeaderNavModules = %s", got)
	}
}
