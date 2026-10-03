package singbox

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestConfigPreservesEditableTopLevelFields(t *testing.T) {
	raw := []byte(`{
 "$schema":"https://example.org/schema.json",
 "ntp":{"enabled":true,"server":"time.example.org"},
 "certificate":{"certificate_store":"system"},
 "certificate_providers":[{"type":"acme","tag":"cert","domain":["example.org"]}],
 "http_clients":[{"tag":"client","detour":"direct"}],
 "network_namespaces":[{"type":"default","tag":"net"}],
 "outbounds":[{"type":"direct","tag":"direct"}]
 }`)
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"$schema", "ntp", "certificate", "certificate_providers", "http_clients", "network_namespaces"} {
		if !reflect.DeepEqual(before[key], after[key]) {
			t.Errorf("%s changed on editor round trip: want %#v, got %#v", key, before[key], after[key])
		}
	}
}
