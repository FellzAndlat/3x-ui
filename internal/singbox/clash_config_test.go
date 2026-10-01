package singbox

import (
	"encoding/json"
	"testing"
)

func TestConfigMarshalAddsPanelClashController(t *testing.T) {
	cfg := NewConfig()
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	experimental, ok := raw["experimental"].(map[string]any)
	if !ok {
		t.Fatalf("experimental missing from config: %s", data)
	}
	clashAPI, ok := experimental["clash_api"].(map[string]any)
	if !ok {
		t.Fatalf("clash_api missing from config: %s", data)
	}
	if got := clashAPI["external_controller"]; got != panelClashController {
		t.Fatalf("external_controller = %v, want %q", got, panelClashController)
	}
}

func TestConfigMarshalPreservesClashAPISettings(t *testing.T) {
	cfg := NewConfig()
	cfg.Experimental = map[string]any{
		"cache_file": map[string]any{"enabled": true},
		"clash_api": map[string]any{
			"external_controller": "127.0.0.1:19090",
			"secret":              "custom-secret",
		},
	}
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	experimental := raw["experimental"].(map[string]any)
	if _, ok := experimental["cache_file"]; !ok {
		t.Fatal("existing experimental setting was lost")
	}
	clashAPI := experimental["clash_api"].(map[string]any)
	if got := clashAPI["external_controller"]; got != "127.0.0.1:19090" {
		t.Fatalf("custom external_controller was overwritten: %v", got)
	}
	if got := clashAPI["secret"]; got != "custom-secret" {
		t.Fatalf("custom secret was overwritten: %v", got)
	}
}

func TestConfigMarshalRejectsMalformedClashAPI(t *testing.T) {
	cfg := NewConfig()
	cfg.Experimental = map[string]any{"clash_api": "invalid"}
	if _, err := cfg.Marshal(); err == nil {
		t.Fatal("expected malformed clash_api to be rejected")
	}
}
