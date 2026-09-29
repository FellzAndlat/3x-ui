package service

import (
	"reflect"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestManagedExtraProtocols(t *testing.T) {
	if got := managedExtraProtocols(model.Shadowsocks); !reflect.DeepEqual(got, []string{"tcp", "udp"}) {
		t.Fatalf("shadowsocks protocols = %#v, want tcp+udp", got)
	}
	if got := managedExtraProtocols(model.VLESS); len(got) != 0 {
		t.Fatalf("vless managed extras = %#v, want none", got)
	}
}

func TestNormalizeFirewallLabel(t *testing.T) {
	if got := normalizeFirewallLabel("  DNS server  "); got != "DNS server" {
		t.Fatalf("label = %q, want %q", got, "DNS server")
	}
	long := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"
	if got := normalizeFirewallLabel(long); len(got) != 120 {
		t.Fatalf("label length = %d, want 120", len(got))
	}
}

func TestNativeFirewallPortExpressions(t *testing.T) {
	rangeRule := FirewallRule{PortRange: "10000-10100", Protocol: "udp"}
	if got := nftPortExpression(rangeRule); got != "10000-10100" {
		t.Fatalf("nft range = %q, want 10000-10100", got)
	}
	if got := iptablesPortExpression(rangeRule); got != "10000:10100" {
		t.Fatalf("iptables range = %q, want 10000:10100", got)
	}

	single := FirewallRule{Port: 443, Protocol: "tcp"}
	if got := nftPortExpression(single); got != "443" {
		t.Fatalf("nft port = %q, want 443", got)
	}
	if got := iptablesPortExpression(single); got != "443" {
		t.Fatalf("iptables port = %q, want 443", got)
	}
}

func TestManualRuleViewsPreserveLabels(t *testing.T) {
	rules := []FirewallManualRule{{Port: 53, Protocol: "udp"}, {Port: 443, Protocol: "tcp"}}
	labels := map[string]string{"53/udp": "DNS", "443/tcp": "HTTPS"}
	got := manualRuleViews(rules, labels)
	if len(got) != 2 {
		t.Fatalf("manual views len = %d, want 2", len(got))
	}
	if got[0].Port != 53 || got[0].Label != "DNS" || got[1].Port != 443 || got[1].Label != "HTTPS" {
		t.Fatalf("unexpected manual views: %#v", got)
	}
}
