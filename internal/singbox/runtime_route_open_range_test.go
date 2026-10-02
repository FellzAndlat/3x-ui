package singbox

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePanelPortSelectorSupportsOpenEndedRanges(t *testing.T) {
	ports, ranges, err := parsePanelPortSelector("53, :3000, 4000:, 1000:2000, :3000")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ports, []int{53}) {
		t.Fatalf("ports = %#v, want [53]", ports)
	}
	wantRanges := []string{":3000", "4000:", "1000:2000"}
	if !reflect.DeepEqual(ranges, wantRanges) {
		t.Fatalf("ranges = %#v, want %#v", ranges, wantRanges)
	}
}

func TestParsePanelPortSelectorRejectsInvalidOpenRanges(t *testing.T) {
	for _, value := range []string{
		":",
		"0:100",
		"100:0",
		"65536:",
		":65536",
		"5000:4000",
		"1:2:3",
	} {
		t.Run(value, func(t *testing.T) {
			_, _, err := parsePanelPortSelector(value)
			if err == nil {
				t.Fatalf("parsePanelPortSelector(%q) unexpectedly succeeded", value)
			}
		})
	}
}

func TestNormalizeRouteForRuntimeMovesOpenRanges(t *testing.T) {
	route := map[string]any{
		"rules": []any{map[string]any{
			"port":        "80, :1023, 49152:",
			"source_port": ":53",
		}},
	}

	normalized, err := normalizeRouteForRuntime(route)
	if err != nil {
		t.Fatal(err)
	}
	rule := normalized["rules"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(rule["port"], []any{float64(80)}) && !reflect.DeepEqual(rule["port"], []int{80}) {
		t.Fatalf("port = %#v, want [80]", rule["port"])
	}
	if !reflect.DeepEqual(rule["port_range"], []string{":1023", "49152:"}) {
		t.Fatalf("port_range = %#v", rule["port_range"])
	}
	if !reflect.DeepEqual(rule["source_port_range"], []string{":53"}) {
		t.Fatalf("source_port_range = %#v", rule["source_port_range"])
	}

	originalRule := route["rules"].([]any)[0].(map[string]any)
	if !strings.Contains(originalRule["port"].(string), ":1023") {
		t.Fatalf("stored route was mutated: %#v", originalRule)
	}
}
