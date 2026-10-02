package singbox

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeRouteForRuntime(t *testing.T) {
	route := map[string]any{
		"final": "direct",
		"rules": []any{
			map[string]any{
				"comment":     "web traffic",
				"port":        "80, 443, 1000:2000, 443",
				"source_port": "53, 5000:5001",
				"action":      "route",
				"outbound":    "proxy",
			},
			map[string]any{
				"type":    "logical",
				"mode":    "or",
				"comment": "nested group",
				"rules": []any{
					map[string]any{
						"comment":    "nested rule",
						"port":       "8080:8081",
						"port_range": []any{"9000:9001"},
					},
				},
			},
		},
	}

	original, err := json.Marshal(route)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeRouteForRuntime(route)
	if err != nil {
		t.Fatal(err)
	}

	if after, err := json.Marshal(route); err != nil || string(after) != string(original) {
		t.Fatalf("normalization mutated source route: before=%s after=%s err=%v", original, after, err)
	}

	rules := normalized["rules"].([]any)
	first := rules[0].(map[string]any)
	if _, exists := first["comment"]; exists {
		t.Fatalf("panel comment leaked into runtime rule: %#v", first)
	}
	if got, want := first["port"], []any{float64(80), float64(443)}; !reflect.DeepEqual(got, want) {
		// normalizeRuntimePortField produces []int before the final config marshal;
		// compare through JSON so this test covers the actual wire representation.
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal([]int{80, 443})
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("port = %s, want %s", gotJSON, wantJSON)
		}
	}
	if gotJSON, _ := json.Marshal(first["port_range"]); string(gotJSON) != `["1000:2000"]` {
		t.Fatalf("port_range = %s", gotJSON)
	}
	if gotJSON, _ := json.Marshal(first["source_port"]); string(gotJSON) != `[53]` {
		t.Fatalf("source_port = %s", gotJSON)
	}
	if gotJSON, _ := json.Marshal(first["source_port_range"]); string(gotJSON) != `["5000:5001"]` {
		t.Fatalf("source_port_range = %s", gotJSON)
	}

	logical := rules[1].(map[string]any)
	if _, exists := logical["comment"]; exists {
		t.Fatalf("logical rule comment leaked into runtime: %#v", logical)
	}
	nested := logical["rules"].([]any)[0].(map[string]any)
	if _, exists := nested["comment"]; exists {
		t.Fatalf("nested rule comment leaked into runtime: %#v", nested)
	}
	if gotJSON, _ := json.Marshal(nested["port_range"]); string(gotJSON) != `["9000:9001","8080:8081"]` {
		t.Fatalf("nested port_range = %s", gotJSON)
	}
	if _, exists := nested["port"]; exists {
		t.Fatalf("range-only selector left an empty port field: %#v", nested)
	}
}

func TestNormalizeRouteForRuntimeRejectsInvalidPanelPorts(t *testing.T) {
	for _, value := range []string{"0", "65536", "abc", "2000:1000", "1:2:3", "80, nope"} {
		t.Run(value, func(t *testing.T) {
			route := map[string]any{
				"rules": []any{map[string]any{"port": value}},
			}
			if _, err := normalizeRouteForRuntime(route); err == nil {
				t.Fatalf("expected %q to be rejected", value)
			}
		})
	}
}

func TestNormalizeRouteForRuntimePreservesNativePortArrays(t *testing.T) {
	route := map[string]any{
		"rules": []any{
			map[string]any{
				"port":       []any{float64(80), float64(443)},
				"port_range": []any{"1000:2000"},
			},
		},
	}
	normalized, err := normalizeRouteForRuntime(route)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(normalized)
	if !strings.Contains(string(got), `"port":[80,443]`) || !strings.Contains(string(got), `"port_range":["1000:2000"]`) {
		t.Fatalf("native route selectors changed unexpectedly: %s", got)
	}
}

func TestConfigMarshalNormalizesPanelRouteMetadata(t *testing.T) {
	cfg := NewConfig()
	cfg.Route = map[string]any{
		"final": "direct",
		"rules": []any{
			map[string]any{
				"comment":  "panel note",
				"port":     "80, 1000:1001",
				"action":   "route",
				"outbound": "direct",
			},
		},
	}
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"comment"`) {
		t.Fatalf("panel-only comment leaked into runtime config: %s", data)
	}
	if !strings.Contains(string(data), `"port": [`) || !strings.Contains(string(data), `"port_range": [`) {
		t.Fatalf("panel port selector was not normalized: %s", data)
	}
	// The editor model must remain untouched after runtime serialization.
	rules := cfg.Route["rules"].([]any)
	if got := rules[0].(map[string]any)["comment"]; got != "panel note" {
		t.Fatalf("stored editor comment changed: %v", got)
	}
	if got := rules[0].(map[string]any)["port"]; got != "80, 1000:1001" {
		t.Fatalf("stored editor port changed: %v", got)
	}
}
