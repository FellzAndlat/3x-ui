package singbox

import "testing"

func TestTranslateNativeSpecialOutboundsDoNotLeakXrayDialFields(t *testing.T) {
	tests := []struct {
		protocol string
		settings map[string]any
	}{
		{protocol: "bridge", settings: map[string]any{}},
		{protocol: "selector", settings: map[string]any{"outbounds": []string{"direct"}}},
		{protocol: "urltest", settings: map[string]any{"outbounds": []string{"direct"}}},
	}

	for _, tc := range tests {
		t.Run(tc.protocol, func(t *testing.T) {
			raw := map[string]any{
				"protocol":    "singbox:" + tc.protocol,
				"tag":         tc.protocol + "-out",
				"settings":    tc.settings,
				"sendThrough": "192.0.2.10",
				"streamSettings": map[string]any{
					"sockopt": map[string]any{
						"interface":   "eth0",
						"dialerProxy": "upstream",
						"mark":        123,
					},
				},
			}

			out, err := translateNativeSingBoxOutbound(raw, raw["protocol"].(string), raw["tag"].(string))
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"inet4_bind_address", "inet6_bind_address", "bind_interface", "detour", "routing_mark", "domain_resolver"} {
				if _, exists := out[key]; exists {
					t.Fatalf("%s leaked unsupported Xray dial field %q: %#v", tc.protocol, key, out)
				}
			}
		})
	}
}

func TestTranslateNativeDirectKeepsXrayDialCompatibility(t *testing.T) {
	raw := map[string]any{
		"protocol":    "singbox:direct",
		"tag":         "direct-out",
		"settings":    map[string]any{},
		"sendThrough": "192.0.2.10",
	}
	out, err := translateNativeSingBoxOutbound(raw, "singbox:direct", "direct-out")
	if err != nil {
		t.Fatal(err)
	}
	if got := rawString(out, "inet4_bind_address"); got != "192.0.2.10" {
		t.Fatalf("native direct lost Xray sendThrough compatibility: %#v", out)
	}
}
