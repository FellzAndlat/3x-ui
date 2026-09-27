package xray

import (
	"encoding/json"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
)

func marshaledInboundSettings(t *testing.T, cfg InboundConfig) map[string]any {
	t.Helper()

	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal inbound: %v", err)
	}
	var wire struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("unmarshal marshaled inbound: %v", err)
	}
	return wire.Settings
}

func TestVlessInboundMarshalDropsEmptyFallbacksWithEncryption(t *testing.T) {
	settings := marshaledInboundSettings(t, InboundConfig{
		Protocol: "vless",
		Settings: json_util.RawMessage(`{
			"clients": [],
			"decryption": "mlkem768x25519plus.native.600s.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			"encryption": "mlkem768x25519plus.native.0rtt.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
			"fallbacks": []
		}`),
	})

	if _, ok := settings["encryption"]; ok {
		t.Fatal("client-side encryption leaked into VLESS inbound settings")
	}
	if _, ok := settings["fallbacks"]; ok {
		t.Fatal("empty fallbacks must be omitted when VLESS decryption is enabled")
	}
	if got, _ := settings["decryption"].(string); got == "" || got == "none" {
		t.Fatalf("decryption was not preserved: %q", got)
	}
}

func TestVlessInboundMarshalKeepsClassicEmptyFallbacks(t *testing.T) {
	settings := marshaledInboundSettings(t, InboundConfig{
		Protocol: "vless",
		Settings: json_util.RawMessage(`{
			"clients": [],
			"decryption": "none",
			"encryption": "none",
			"fallbacks": []
		}`),
	})

	if _, ok := settings["encryption"]; ok {
		t.Fatal("client-side encryption leaked into classic VLESS inbound settings")
	}
	fallbacks, ok := settings["fallbacks"].([]any)
	if !ok || len(fallbacks) != 0 {
		t.Fatalf("classic VLESS fallbacks changed unexpectedly: %#v", settings["fallbacks"])
	}
}

func TestVlessInboundMarshalPreservesNonEmptyFallbacks(t *testing.T) {
	settings := marshaledInboundSettings(t, InboundConfig{
		Protocol: "vless",
		Settings: json_util.RawMessage(`{
			"clients": [],
			"decryption": "mlkem768x25519plus.native.600s.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			"fallbacks": [{"dest": 8080}]
		}`),
	})

	fallbacks, ok := settings["fallbacks"].([]any)
	if !ok || len(fallbacks) != 1 {
		t.Fatalf("non-empty fallbacks must be preserved for core validation: %#v", settings["fallbacks"])
	}
}

func TestNonVlessInboundMarshalLeavesSettingsUntouched(t *testing.T) {
	settings := marshaledInboundSettings(t, InboundConfig{
		Protocol: "vmess",
		Settings: json_util.RawMessage(`{"encryption":"keep-me","fallbacks":[]}`),
	})

	if settings["encryption"] != "keep-me" {
		t.Fatalf("non-VLESS settings were modified: %#v", settings)
	}
	if _, ok := settings["fallbacks"]; !ok {
		t.Fatalf("non-VLESS fallbacks were removed: %#v", settings)
	}
}
