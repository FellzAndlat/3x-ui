package singbox

import (
	"strings"
	"testing"
)

func TestTranslateXrayShadowsocksRequiresMethodAndPassword(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"protocol": "shadowsocks",
			"tag":      "ss",
			"settings": map[string]any{
				"servers": []any{map[string]any{
					"address":  "ss.example.com",
					"port":     8388,
					"method":   "aes-256-gcm",
					"password": "secret",
				}},
			},
		}
	}

	missingMethod := base()
	delete(missingMethod["settings"].(map[string]any)["servers"].([]any)[0].(map[string]any), "method")
	if _, err := TranslateXrayOutbound(missingMethod); err == nil || !strings.Contains(err.Error(), "method") {
		t.Fatalf("missing method error = %v", err)
	}

	missingPassword := base()
	delete(missingPassword["settings"].(map[string]any)["servers"].([]any)[0].(map[string]any), "password")
	if _, err := TranslateXrayOutbound(missingPassword); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("missing password error = %v", err)
	}
}

func TestTranslateXrayShadowsocksPreservesUDPOverTCP(t *testing.T) {
	raw := map[string]any{
		"protocol": "shadowsocks",
		"tag":      "ss-uot",
		"settings": map[string]any{
			"servers": []any{map[string]any{
				"address":    "ss.example.com",
				"port":       8388,
				"method":     "aes-256-gcm",
				"password":   "secret",
				"uot":        true,
				"UoTVersion": 2,
			}},
		},
	}
	got, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	uot, ok := got["udp_over_tcp"].(map[string]any)
	if !ok || uot["enabled"] != true || uot["version"] != 2 {
		t.Fatalf("udp_over_tcp = %#v", got["udp_over_tcp"])
	}

	raw["settings"].(map[string]any)["servers"].([]any)[0].(map[string]any)["UoTVersion"] = 3
	if _, err := TranslateXrayOutbound(raw); err == nil || !strings.Contains(err.Error(), "UoTVersion") {
		t.Fatalf("invalid UoTVersion error = %v", err)
	}
}

func TestTranslateXrayTUICRequiresUUIDAndEnablesTLS(t *testing.T) {
	base := map[string]any{
		"protocol": "tuic",
		"tag":      "tuic",
		"settings": map[string]any{
			"servers": []any{map[string]any{
				"address":  "tuic.example.com",
				"port":     443,
				"uuid":     "2DD61D93-75D8-4DA4-AC0E-6AECE7EAC365",
				"password": "secret",
			}},
		},
	}
	got, err := TranslateXrayOutbound(base)
	if err != nil {
		t.Fatal(err)
	}
	tls, ok := got["tls"].(map[string]any)
	if !ok || tls["enabled"] != true {
		t.Fatalf("TUIC TLS = %#v", got["tls"])
	}

	delete(base["settings"].(map[string]any)["servers"].([]any)[0].(map[string]any), "uuid")
	if _, err := TranslateXrayOutbound(base); err == nil || !strings.Contains(err.Error(), "UUID") {
		t.Fatalf("missing TUIC UUID error = %v", err)
	}
}

func TestTranslateXrayOutboundRejectsOversizedServerPort(t *testing.T) {
	raw := map[string]any{
		"protocol": "http",
		"tag":      "bad-port",
		"settings": map[string]any{
			"servers": []any{map[string]any{
				"address": "proxy.example.com",
				"port":    70000,
			}},
		},
	}
	if _, err := TranslateXrayOutbound(raw); err == nil || !strings.Contains(err.Error(), "server port") {
		t.Fatalf("oversized port error = %v", err)
	}
}
