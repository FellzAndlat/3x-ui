package singbox

import (
	"strings"
	"testing"
)

func TestTranslateXrayOutboundNativeAnyTLS(t *testing.T) {
	raw := map[string]any{
		"protocol": "AnYtLs",
		"tag":      "anytls-out",
		"settings": map[string]any{
			"type":        "direct",
			"tag":         "spoofed-tag",
			"server":      "vpn.example.com",
			"server_port": 443,
			"password":    "secret",
			"tls":         map[string]any{"enabled": true, "server_name": "vpn.example.com"},
		},
		"streamSettings": map[string]any{
			"network": "ws",
		},
		"mux": map[string]any{"enabled": false},
	}

	out, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatalf("TranslateXrayOutbound() error = %v", err)
	}
	if got := rawString(out, "type"); got != "anytls" {
		t.Fatalf("type = %q, want anytls", got)
	}
	if got := rawString(out, "tag"); got != "anytls-out" {
		t.Fatalf("tag = %q, want anytls-out", got)
	}
	if got := rawString(out, "server"); got != "vpn.example.com" {
		t.Fatalf("server = %q, want vpn.example.com", got)
	}
	if got := rawInt(out, "server_port"); got != 443 {
		t.Fatalf("server_port = %d, want 443", got)
	}
	if got := rawString(out, "password"); got != "secret" {
		t.Fatalf("password = %q, want secret", got)
	}
	if got := rawString(out, "domain_resolver"); got != "local" {
		t.Fatalf("domain_resolver = %q, want local", got)
	}
	for _, key := range []string{"protocol", "settings", "streamSettings", "mux"} {
		if _, exists := out[key]; exists {
			t.Errorf("translated outbound leaked Xray wrapper field %q", key)
		}
	}

	config := &Config{Outbounds: []map[string]any{out}}
	if _, err := config.Marshal(); err != nil {
		t.Fatalf("translated AnyTLS outbound does not pass sing-box validation: %v", err)
	}
}

func TestTranslateXrayOutboundNativeSelector(t *testing.T) {
	raw := map[string]any{
		"protocol": "selector",
		"tag":      "auto-proxy",
		"settings": map[string]any{
			"outbounds":                   []any{"proxy-a", "proxy-b"},
			"default":                     "proxy-a",
			"interrupt_exist_connections": true,
		},
	}

	out, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatalf("TranslateXrayOutbound() error = %v", err)
	}
	if got := rawString(out, "type"); got != "selector" {
		t.Fatalf("type = %q, want selector", got)
	}
	if got := rawString(out, "default"); got != "proxy-a" {
		t.Fatalf("default = %q, want proxy-a", got)
	}
	if got := stringSliceLength(out["outbounds"]); got != 2 {
		t.Fatalf("outbounds length = %d, want 2", got)
	}

	config := &Config{Outbounds: []map[string]any{out}}
	if _, err := config.Marshal(); err != nil {
		t.Fatalf("translated selector outbound does not pass sing-box validation: %v", err)
	}
}

func TestTranslateXrayOutboundFutureProtocolPassThrough(t *testing.T) {
	raw := map[string]any{
		"protocol": "future-protocol",
		"tag":      "future-out",
		"settings": map[string]any{
			"future_option": "preserved",
			"enabled":       true,
		},
	}

	out, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatalf("TranslateXrayOutbound() error = %v", err)
	}
	if got := rawString(out, "type"); got != "future-protocol" {
		t.Fatalf("type = %q, want future-protocol", got)
	}
	if got := rawString(out, "future_option"); got != "preserved" {
		t.Fatalf("future_option = %q, want preserved", got)
	}

	config := &Config{Outbounds: []map[string]any{out}}
	if _, err := config.Marshal(); err != nil {
		t.Fatalf("future outbound should remain pass-through compatible: %v", err)
	}
}

func TestTranslateXrayOutboundExplicitNativeAliases(t *testing.T) {
	tests := []struct {
		wrapper  string
		expected string
		settings map[string]any
	}{
		{
			wrapper: "singbox:tuic",
			expected: "tuic",
			settings: map[string]any{
				"server": "tuic.example.com", "server_port": 443,
				"uuid": "2a547fe4-5b0f-4a84-845c-131464875bf8", "password": "secret",
				"tls": map[string]any{"enabled": true, "server_name": "tuic.example.com"},
			},
		},
		{
			wrapper: "singbox:hysteria2",
			expected: "hysteria2",
			settings: map[string]any{
				"server": "hy2.example.com", "server_port": 443, "password": "secret",
				"tls": map[string]any{"enabled": true, "server_name": "hy2.example.com"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			out, err := TranslateXrayOutbound(map[string]any{
				"protocol": tc.wrapper,
				"tag":      tc.expected + "-native",
				"settings": tc.settings,
			})
			if err != nil {
				t.Fatalf("TranslateXrayOutbound() error = %v", err)
			}
			if got := rawString(out, "type"); got != tc.expected {
				t.Fatalf("type = %q, want %q", got, tc.expected)
			}
			config := &Config{Outbounds: []map[string]any{out}}
			if _, err := config.Marshal(); err != nil {
				t.Fatalf("translated native alias does not pass validation: %v", err)
			}
		})
	}
}

func TestTranslateXrayOutboundNativeAppliesCommonDialerOptions(t *testing.T) {
	raw := map[string]any{
		"protocol":   "ssh",
		"tag":        "ssh-out",
		"sendThrough": "192.0.2.10",
		"settings": map[string]any{
			"server":      "ssh.example.com",
			"server_port": 22,
			"user":        "proxy",
		},
		"streamSettings": map[string]any{
			"sockopt": map[string]any{
				"interface":   "eth0",
				"tcpFastOpen": true,
				"dialerProxy": "bootstrap",
			},
		},
	}

	out, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatalf("TranslateXrayOutbound() error = %v", err)
	}
	if got := rawString(out, "inet4_bind_address"); got != "192.0.2.10" {
		t.Fatalf("inet4_bind_address = %q, want 192.0.2.10", got)
	}
	if got := rawString(out, "bind_interface"); got != "eth0" {
		t.Fatalf("bind_interface = %q, want eth0", got)
	}
	if enabled, _ := out["tcp_fast_open"].(bool); !enabled {
		t.Fatal("tcp_fast_open was not translated")
	}
	if got := rawString(out, "detour"); got != "bootstrap" {
		t.Fatalf("detour = %q, want bootstrap", got)
	}
}

func TestTranslateXrayOutboundNativeRejectsInvalidSettings(t *testing.T) {
	_, err := TranslateXrayOutbound(map[string]any{
		"protocol": "anytls",
		"tag":      "broken",
		"settings": []any{"not", "an", "object"},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid settings") {
		t.Fatalf("expected invalid settings error, got %v", err)
	}
}

func TestTranslateXrayOutboundNativeRejectsEmptyGroup(t *testing.T) {
	for _, protocol := range []string{"selector", "urltest"} {
		t.Run(protocol, func(t *testing.T) {
			_, err := TranslateXrayOutbound(map[string]any{
				"protocol": protocol,
				"tag":      protocol + "-out",
				"settings": map[string]any{"outbounds": []any{}},
			})
			if err == nil || !strings.Contains(err.Error(), "requires at least one outbound tag") {
				t.Fatalf("expected empty group validation error, got %v", err)
			}
		})
	}
}

func TestTranslateXrayOutboundNativeRejectsEndpointType(t *testing.T) {
	_, err := TranslateXrayOutbound(map[string]any{
		"protocol": "singbox:tailscale",
		"tag":      "tailscale-out",
		"settings": map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "configure it as an endpoint") {
		t.Fatalf("expected endpoint type validation error, got %v", err)
	}
}
