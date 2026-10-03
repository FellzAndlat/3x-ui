package singbox

import "testing"

func TestTranslateNativeSingBoxStableOutboundMatrix(t *testing.T) {
	tests := []struct {
		protocol string
		settings map[string]any
	}{
		{
			protocol: "bridge",
			settings: map[string]any{"interface": "eth0"},
		},
		{
			protocol: "shadowtls",
			settings: map[string]any{
				"server": "shadow.example.com", "server_port": 443,
				"version": 3, "password": "secret",
				"tls": map[string]any{"enabled": true, "server_name": "shadow.example.com"},
			},
		},
		{
			protocol: "anytls",
			settings: map[string]any{
				"server": "anytls.example.com", "server_port": 443, "password": "secret",
				"tls": map[string]any{"enabled": true, "server_name": "anytls.example.com"},
			},
		},
		{
			protocol: "snell",
			settings: map[string]any{
				"server": "snell.example.com", "server_port": 443, "version": 4, "psk": "secret",
			},
		},
		{
			protocol: "tor",
			settings: map[string]any{},
		},
		{
			protocol: "ssh",
			settings: map[string]any{"server": "ssh.example.com", "user": "proxy"},
		},
		{
			protocol: "selector",
			settings: map[string]any{"outbounds": []any{"direct", "proxy"}, "default": "proxy"},
		},
		{
			protocol: "urltest",
			settings: map[string]any{
				"outbounds": []any{"proxy-a", "proxy-b"},
				"url": "https://www.gstatic.com/generate_204", "interval": "3m",
			},
		},
		{
			protocol: "naive",
			settings: map[string]any{
				"server": "naive.example.com", "server_port": 443,
				"username": "proxy", "password": "secret",
				"tls": map[string]any{"enabled": true, "server_name": "naive.example.com"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.protocol, func(t *testing.T) {
			raw := map[string]any{
				"protocol": tc.protocol,
				"tag":      tc.protocol + "-out",
				"settings": tc.settings,
			}
			out, err := TranslateXrayOutbound(raw)
			if err != nil {
				t.Fatalf("TranslateXrayOutbound() error = %v", err)
			}
			if got := rawString(out, "type"); got != tc.protocol {
				t.Fatalf("type = %q, want %q", got, tc.protocol)
			}
			if got := rawString(out, "tag"); got != tc.protocol+"-out" {
				t.Fatalf("tag = %q, want %q", got, tc.protocol+"-out")
			}
			config := &Config{Outbounds: []map[string]any{out}}
			if _, err := config.Marshal(); err != nil {
				t.Fatalf("translated outbound does not pass validation: %v", err)
			}
		})
	}
}
