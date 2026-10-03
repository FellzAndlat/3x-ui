package singbox

import (
	"encoding/json"
	"reflect"
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

func TestNormalizeV2RayTransportForRuntimeRemovesOnlyKnownIncompatibleFields(t *testing.T) {
	tests := []struct {
		name      string
		typeName  string
		wantKeys  []string
		dropKeys  []string
	}{
		{
			name:     "websocket after grpc",
			typeName: "ws",
			wantKeys: []string{"type", "path", "headers", "max_early_data", "early_data_header_name", "future_option"},
			dropKeys: []string{"host", "method", "idle_timeout", "ping_timeout", "service_name", "permit_without_stream"},
		},
		{
			name:     "grpc after http",
			typeName: "grpc",
			wantKeys: []string{"type", "service_name", "idle_timeout", "ping_timeout", "permit_without_stream", "future_option"},
			dropKeys: []string{"host", "path", "method", "headers", "max_early_data", "early_data_header_name"},
		},
		{
			name:     "httpupgrade after grpc",
			typeName: "httpupgrade",
			wantKeys: []string{"type", "host", "path", "headers", "future_option"},
			dropKeys: []string{"method", "idle_timeout", "ping_timeout", "max_early_data", "early_data_header_name", "service_name", "permit_without_stream"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			transport := map[string]any{
				"type":                  tc.typeName,
				"host":                  []any{"cdn.example.com"},
				"path":                  "/edge",
				"method":                "PUT",
				"headers":               map[string]any{"X-Test": "1"},
				"idle_timeout":          "30s",
				"ping_timeout":          "10s",
				"max_early_data":        float64(2048),
				"early_data_header_name": "Sec-WebSocket-Protocol",
				"service_name":          "grpc-service",
				"permit_without_stream": true,
				"future_option":         "preserve-me",
			}
			if err := normalizeV2RayTransportForRuntime(transport, tc.typeName); err != nil {
				t.Fatal(err)
			}
			for _, key := range tc.wantKeys {
				if _, ok := transport[key]; !ok {
					t.Errorf("expected %q to be preserved: %#v", key, transport)
				}
			}
			for _, key := range tc.dropKeys {
				if _, ok := transport[key]; ok {
					t.Errorf("expected stale %q to be removed: %#v", key, transport)
				}
			}
		})
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

func TestNormalizeV2RayTransportForRuntimeLeavesUnknownTransportUntouched(t *testing.T) {
	transport := map[string]any{
		"type":         "future-transport",
		"service_name": "keep",
		"future_option": true,
	}
	before := map[string]any{}
	for key, value := range transport {
		before[key] = value
	}
	if err := normalizeV2RayTransportForRuntime(transport, "future-transport"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(transport, before) {
		t.Fatalf("unknown transport was modified: before=%#v after=%#v", before, transport)
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
