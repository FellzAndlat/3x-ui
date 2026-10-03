package adblock

import (
	"encoding/json"
	"testing"
)

func TestXraySniffingRecoversDomainsWithoutRedirecting(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte(`{"enabled":false,"metadataOnly":true}`)} {
		got, err := XraySniffing(raw)
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			Enabled, RouteOnly, MetadataOnly bool
			DestOverride                     []string
		}
		if err := json.Unmarshal(got, &config); err != nil {
			t.Fatal(err)
		}
		if !config.Enabled || !config.RouteOnly || config.MetadataOnly || len(config.DestOverride) != 3 {
			t.Fatalf("bad sniffing: %s", got)
		}
	}
}

func TestXraySniffingPreservesExistingOptions(t *testing.T) {
	got, err := XraySniffing([]byte(`{"enabled":true,"routeOnly":false,"destOverride":["fakedns","tls"],"domainsExcluded":["example.com"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	_ = json.Unmarshal(got, &config)
	if config["routeOnly"] != false || len(config["domainsExcluded"].([]any)) != 1 || len(config["destOverride"].([]any)) != 4 {
		t.Fatalf("lost options: %s", got)
	}
}
