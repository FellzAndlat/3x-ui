package singbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDNSVirtualInboundPolicy(t *testing.T) {
	var dns map[string]any
	if err := json.Unmarshal([]byte(`{"tag":"dns-global","servers":[{"address":"1.1.1.1","tag":"dns-server"}]}`), &dns); err != nil {
		t.Fatal(err)
	}
	if _, err := TranslateXrayDNS(dns); err != nil {
		t.Fatalf("unused tags must not block DNS translation: %v", err)
	}
	for _, tag := range []string{"dns-global", "dns-server", "client-inbound"} {
		routing := map[string]any{"rules": []any{map[string]any{"inboundTag": []string{tag}, "outboundTag": "proxy"}}}
		err := ValidateXrayDNSRouting(dns, routing)
		if tag == "client-inbound" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), tag) {
			t.Fatalf("virtual inbound policy %q: %v", tag, err)
		}
	}
	if err := ValidateXrayDNSRouting(dns, nil); err != nil {
		t.Fatal(err)
	}
}
