package singbox

import (
	"strings"
	"testing"
)

func TestNativeOutboundGroupValidation(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		tag      string
		settings map[string]any
		wantErr  string
	}{
		{
			name:     "selector valid default",
			protocol: "selector",
			tag:      "select",
			settings: map[string]any{"outbounds": []any{"proxy-a", "proxy-b"}, "default": "proxy-b"},
		},
		{
			name:     "urltest valid",
			protocol: "urltest",
			tag:      "auto",
			settings: map[string]any{"outbounds": []any{"proxy-a", "proxy-b"}},
		},
		{
			name:     "non string member",
			protocol: "selector",
			tag:      "select",
			settings: map[string]any{"outbounds": []any{"proxy-a", 7}},
			wantErr:  "invalid outbound tag",
		},
		{
			name:     "empty member",
			protocol: "urltest",
			tag:      "auto",
			settings: map[string]any{"outbounds": []any{"proxy-a", "   "}},
			wantErr:  "invalid outbound tag",
		},
		{
			name:     "self reference",
			protocol: "selector",
			tag:      "select",
			settings: map[string]any{"outbounds": []any{"proxy-a", "select"}},
			wantErr:  "cannot reference itself",
		},
		{
			name:     "duplicate member",
			protocol: "urltest",
			tag:      "auto",
			settings: map[string]any{"outbounds": []any{"proxy-a", "proxy-a"}},
			wantErr:  "duplicate outbound tag",
		},
		{
			name:     "selector default outside members",
			protocol: "selector",
			tag:      "select",
			settings: map[string]any{"outbounds": []any{"proxy-a", "proxy-b"}, "default": "proxy-c"},
			wantErr:  "is not present in outbounds",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := TranslateXrayOutbound(map[string]any{
				"protocol": tc.protocol,
				"tag":      tc.tag,
				"settings": tc.settings,
			})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("TranslateXrayOutbound() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestMarshalValidatesNativeGroups(t *testing.T) {
	for _, settings := range []map[string]any{
		{"outbounds": []any{}},
		{"outbounds": []any{"direct"}, "default": 123},
		{"outbounds": []any{" direct "}},
	} {
		out := map[string]any{"type": "selector", "tag": "group"}
		for key, value := range settings {
			out[key] = value
		}
		if _, err := (&Config{Outbounds: []map[string]any{out}}).Marshal(); err == nil {
			t.Fatalf("accepted invalid group: %v", out)
		}
	}
}
