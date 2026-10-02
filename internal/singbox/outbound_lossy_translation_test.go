package singbox

import (
	"strings"
	"testing"
)

func TestTranslateFreedomRejectsUnsupportedExtensions(t *testing.T) {
	cases := []struct {
		name     string
		settings map[string]any
		stream   map[string]any
		want     string
	}{
		{name: "redirect", settings: map[string]any{"redirect": "1.1.1.1:443"}, want: "redirect"},
		{name: "proxy protocol", settings: map[string]any{"proxyProtocol": 1}, want: "proxyProtocol"},
		{name: "fragment", settings: map[string]any{"fragment": map[string]any{"length": "10-20"}}, want: "fragment"},
		{name: "noises", settings: map[string]any{"noises": []any{map[string]any{"type": "rand"}}}, want: "noises"},
		{name: "final rules", settings: map[string]any{"finalRules": []any{map[string]any{"action": "block"}}}, want: "finalRules"},
		{name: "domain strategy", settings: map[string]any{}, stream: map[string]any{"sockopt": map[string]any{"domainStrategy": "UseIPv4"}}, want: "domainStrategy"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]any{
				"protocol": "freedom",
				"tag":      "direct",
				"settings": tc.settings,
			}
			if tc.stream != nil {
				raw["streamSettings"] = tc.stream
			}
			_, err := TranslateXrayOutbound(raw)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want diagnostic containing %q", err, tc.want)
			}
		})
	}
}

func TestTranslateFreedomAcceptsNoOpSettings(t *testing.T) {
	raw := map[string]any{
		"protocol": "freedom",
		"tag":      "direct",
		"settings": map[string]any{
			"userLevel":     0,
			"proxyProtocol": 0,
		},
		"streamSettings": map[string]any{
			"sockopt": map[string]any{"domainStrategy": "AsIs"},
		},
	}
	got, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "direct" || got["tag"] != "direct" {
		t.Fatalf("translated freedom = %#v", got)
	}
}

func TestTranslateBlackholeRejectsResponseModesNotRepresentableByBlock(t *testing.T) {
	for _, responseType := range []string{"http", "custom"} {
		t.Run(responseType, func(t *testing.T) {
			raw := map[string]any{
				"protocol": "blackhole",
				"tag":      "blocked",
				"settings": map[string]any{
					"response": map[string]any{"type": responseType, "customResponseData": "HTTP/1.1 403 Forbidden"},
				},
			}
			_, err := TranslateXrayOutbound(raw)
			if err == nil || !strings.Contains(err.Error(), "response") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestTranslateBlackholeAcceptsDefaultAndNoneResponse(t *testing.T) {
	for _, settings := range []map[string]any{
		{},
		{"response": map[string]any{"type": "none"}},
	} {
		got, err := TranslateXrayOutbound(map[string]any{
			"protocol": "blackhole",
			"tag":      "blocked",
			"settings": settings,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got["type"] != "block" {
			t.Fatalf("translated blackhole = %#v", got)
		}
	}
}
