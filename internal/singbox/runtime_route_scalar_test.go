package singbox

import (
	"encoding/json"
	"testing"
)

func TestNormalizeRouteForRuntimeMergesScalarRange(t *testing.T) {
	route := map[string]any{
		"rules": []any{map[string]any{
			"port":       "80, 1000:1001",
			"port_range": "9000:9001",
		}},
	}

	normalized, err := normalizeRouteForRuntime(route)
	if err != nil {
		t.Fatal(err)
	}
	rule := normalized["rules"].([]any)[0].(map[string]any)

	gotPorts, _ := json.Marshal(rule["port"])
	if string(gotPorts) != `[80]` {
		t.Fatalf("port = %s, want [80]", gotPorts)
	}
	gotRanges, _ := json.Marshal(rule["port_range"])
	if string(gotRanges) != `["9000:9001","1000:1001"]` {
		t.Fatalf("port_range = %s", gotRanges)
	}

	original := route["rules"].([]any)[0].(map[string]any)
	if original["port"] != "80, 1000:1001" || original["port_range"] != "9000:9001" {
		t.Fatalf("stored editor route mutated: %#v", original)
	}
}
