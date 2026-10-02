package singbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeOutboundsForRuntimeRejectsInboundTypes(t *testing.T) {
	for _, outboundType := range []string{"tun", "redirect", "tproxy"} {
		t.Run(outboundType, func(t *testing.T) {
			_, err := normalizeOutboundsForRuntime([]map[string]any{{
				"type": outboundType,
				"tag":  "bad",
			}})
			if err == nil || !strings.Contains(err.Error(), "inbound type") {
				t.Fatalf("expected inbound-only type %q to be rejected, got %v", outboundType, err)
			}
		})
	}
}

func TestNormalizeOutboundsForRuntimeConvertsHTTPTransportHost(t *testing.T) {
	outbounds := []map[string]any{{
		"type": "vless",
		"tag":  "proxy",
		"transport": map[string]any{
			"type": "http",
			"path": "/edge",
			"headers": map[string]any{
				"Host":       "cdn.example.com",
				"User-Agent": "panel-test",
			},
		},
	}}
	original, _ := json.Marshal(outbounds)

	normalized, err := normalizeOutboundsForRuntime(outbounds)
	if err != nil {
		t.Fatal(err)
	}
	transport := normalized[0]["transport"].(map[string]any)
	gotHost, _ := json.Marshal(transport["host"])
	if string(gotHost) != `["cdn.example.com"]` {
		t.Fatalf("HTTP transport host = %s", gotHost)
	}
	headers := transport["headers"].(map[string]any)
	if _, exists := headers["Host"]; exists {
		t.Fatalf("legacy Host header was not removed: %#v", headers)
	}
	if headers["User-Agent"] != "panel-test" {
		t.Fatalf("unrelated header was changed: %#v", headers)
	}

	after, _ := json.Marshal(outbounds)
	if string(after) != string(original) {
		t.Fatalf("normalization mutated stored outbound: before=%s after=%s", original, after)
	}
}

func TestNormalizeOutboundsForRuntimeStripsStaleQuicOptions(t *testing.T) {
	outbounds := []map[string]any{{
		"type": "vless",
		"tag":  "proxy",
		"transport": map[string]any{
			"type":                  "quic",
			"service_name":          "old-grpc",
			"path":                  "/old-http",
			"host":                  []any{"cdn.example.com"},
			"headers":               map[string]any{"User-Agent": "panel-test"},
			"idle_timeout":          "30s",
			"permit_without_stream": true,
		},
	}}
	original, _ := json.Marshal(outbounds)

	normalized, err := normalizeOutboundsForRuntime(outbounds)
	if err != nil {
		t.Fatal(err)
	}
	transport := normalized[0]["transport"].(map[string]any)
	if len(transport) != 1 || transport["type"] != "quic" {
		t.Fatalf("stale QUIC options leaked into runtime: %#v", transport)
	}

	after, _ := json.Marshal(outbounds)
	if string(after) != string(original) {
		t.Fatalf("normalization mutated stored outbound: before=%s after=%s", original, after)
	}
}

func TestNormalizeOutboundsForRuntimeKeepsWebSocketHostHeader(t *testing.T) {
	outbounds := []map[string]any{{
		"type": "vless",
		"tag":  "proxy",
		"transport": map[string]any{
			"type":    "ws",
			"headers": map[string]any{"Host": "cdn.example.com"},
		},
	}}
	normalized, err := normalizeOutboundsForRuntime(outbounds)
	if err != nil {
		t.Fatal(err)
	}
	transport := normalized[0]["transport"].(map[string]any)
	headers := transport["headers"].(map[string]any)
	if headers["Host"] != "cdn.example.com" {
		t.Fatalf("WebSocket Host header changed: %#v", headers)
	}
	if _, exists := transport["host"]; exists {
		t.Fatalf("WebSocket Host header was incorrectly converted: %#v", transport)
	}
}

func TestConfigMarshalAppliesOutboundNormalization(t *testing.T) {
	cfg := NewConfig()
	cfg.Outbounds = []map[string]any{{
		"type": "vless",
		"tag":  "proxy",
		"transport": map[string]any{
			"type":    "http",
			"headers": map[string]any{"Host": "cdn.example.com"},
		},
	}}
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"Host"`) || !strings.Contains(string(data), `"host": [`) {
		t.Fatalf("HTTP transport Host was not normalized in runtime JSON: %s", data)
	}

	cfg.Outbounds = []map[string]any{{"type": "tun", "tag": "bad"}}
	if _, err := cfg.Marshal(); err == nil || !strings.Contains(err.Error(), "inbound type") {
		t.Fatalf("expected Config.Marshal to reject inbound-only outbound type, got %v", err)
	}
}

func TestNormalizeRouteForRuntimeRejectsSelectorAction(t *testing.T) {
	route := map[string]any{
		"rules": []any{map[string]any{
			"action":   "selector",
			"outbound": "proxy",
		}},
	}
	_, err := normalizeRouteForRuntime(route)
	if err == nil || !strings.Contains(err.Error(), "not a sing-box route action") {
		t.Fatalf("expected selector route action to be rejected, got %v", err)
	}
}
