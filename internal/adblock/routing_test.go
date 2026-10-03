package adblock

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestApplyXrayRoutingAddsPriorityRuleAndBlackhole(t *testing.T) {
	router := []byte(`{"domainStrategy":"AsIs","rules":[{"type":"field","network":"tcp","outboundTag":"direct"}]}`)
	outbounds := []byte(`[{"tag":"direct","protocol":"freedom"}]`)

	gotRouter, gotOutbounds, err := ApplyXrayRouting(router, outbounds, []string{"ads.example.com", "ADS.EXAMPLE.COM", "tracker.example.net"})
	if err != nil {
		t.Fatalf("ApplyXrayRouting() error = %v", err)
	}

	var route struct {
		Rules []struct {
			Domain      []string `json:"domain"`
			OutboundTag string   `json:"outboundTag"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(gotRouter, &route); err != nil {
		t.Fatal(err)
	}
	if len(route.Rules) != 2 || route.Rules[0].OutboundTag != OutboundTag {
		t.Fatalf("managed adblock rule is not first: %s", gotRouter)
	}
	if len(route.Rules[0].Domain) != 2 || route.Rules[0].Domain[0] != "full:ads.example.com" {
		t.Fatalf("unexpected domains: %#v", route.Rules[0].Domain)
	}

	var outs []struct {
		Tag      string `json:"tag"`
		Protocol string `json:"protocol"`
	}
	if err := json.Unmarshal(gotOutbounds, &outs); err != nil {
		t.Fatal(err)
	}
	if len(outs) != 2 || outs[1].Tag != OutboundTag || outs[1].Protocol != "blackhole" {
		t.Fatalf("unexpected outbounds: %s", gotOutbounds)
	}

	secondRouter, secondOutbounds, err := ApplyXrayRouting(gotRouter, gotOutbounds, []string{"tracker.example.net", "ads.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if string(secondRouter) != string(gotRouter) || string(secondOutbounds) != string(gotOutbounds) {
		t.Fatalf("ApplyXrayRouting() is not idempotent\nfirst:  %s %s\nsecond: %s %s", gotRouter, gotOutbounds, secondRouter, secondOutbounds)
	}
}

func TestApplyXrayRoutingDisablesManagedEntries(t *testing.T) {
	router, outbounds, err := ApplyXrayRouting(nil, nil, []string{"ads.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	router, outbounds, err = ApplyXrayRouting(router, outbounds, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(outbounds) != `[{"protocol":"freedom"}]` {
		t.Fatalf("unexpected outbounds after disable: %s", outbounds)
	}
	var route struct {
		Rules []json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(router, &route); err != nil {
		t.Fatal(err)
	}
	if len(route.Rules) != 0 {
		t.Fatalf("unexpected rules after disable: %s", router)
	}
}

func TestApplyXrayRoutingRejectsTagConflict(t *testing.T) {
	_, _, err := ApplyXrayRouting(nil, []byte(`[{"tag":"3x-ui-adblock","protocol":"freedom"}]`), []string{"ads.example.com"})
	if !errors.Is(err, ErrOutboundTagConflict) {
		t.Fatalf("error = %v, want %v", err, ErrOutboundTagConflict)
	}
}

func TestEmptyOutboundsKeepsFreedomAsDefault(t *testing.T) {
	_, outbounds, err := ApplyXrayRouting(nil, nil, []string{"ads.example.com", "domain:tracker.example"})
	if err != nil {
		t.Fatal(err)
	}
	var outs []map[string]any
	if err := json.Unmarshal(outbounds, &outs); err != nil {
		t.Fatal(err)
	}
	if len(outs) != 2 || outs[0]["protocol"] != "freedom" || outs[1]["protocol"] != "blackhole" {
		t.Fatalf("unsafe default: %s", outbounds)
	}
}
