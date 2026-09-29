package service

import (
	"reflect"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/mieru"
)

func TestFirewallProtocolsForInbound(t *testing.T) {
	tests := []struct {
		name     string
		protocol model.Protocol
		network  string
		want     []string
	}{
		{name: "vless tcp", protocol: model.VLESS, network: "tcp", want: []string{"tcp"}},
		{name: "vless websocket", protocol: model.VLESS, network: "ws", want: []string{"tcp"}},
		{name: "vless kcp", protocol: model.VLESS, network: "kcp", want: []string{"udp"}},
		{name: "mixed", protocol: model.Mixed, network: "tcp", want: []string{"tcp", "udp"}},
		{name: "wireguard", protocol: model.WireGuard, network: "tcp", want: []string{"udp"}},
		{name: "amneziawg", protocol: model.AmneziaWG, network: "tcp", want: []string{"udp"}},
		{name: "hysteria", protocol: model.Hysteria, network: "", want: []string{"udp"}},
		{name: "tuic", protocol: model.TUIC, network: "", want: []string{"udp"}},
		{name: "combined", protocol: model.VLESS, network: "tcp,udp", want: []string{"tcp", "udp"}},
		{name: "unknown defaults tcp", protocol: model.VLESS, network: "", want: []string{"tcp"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firewallProtocolsForInbound(tt.protocol, tt.network)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("firewallProtocolsForInbound(%q, %q) = %#v, want %#v", tt.protocol, tt.network, got, tt.want)
			}
		})
	}
}

func TestNormalizeFirewallProtocols(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{input: "tcp", want: []string{"tcp"}},
		{input: "UDP", want: []string{"udp"}},
		{input: "both", want: []string{"tcp", "udp"}},
		{input: "udp,tcp", want: []string{"tcp", "udp"}},
	}
	for _, tt := range tests {
		got, err := normalizeFirewallProtocols(tt.input)
		if err != nil {
			t.Fatalf("normalizeFirewallProtocols(%q): %v", tt.input, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("normalizeFirewallProtocols(%q) = %#v, want %#v", tt.input, got, tt.want)
		}
	}
	if _, err := normalizeFirewallProtocols("icmp"); err == nil {
		t.Fatal("expected invalid protocol error")
	}
}

func TestCanonicalFirewallSpec(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "22/tcp", want: "22/tcp", ok: true},
		{input: "443/TCP", want: "443/tcp", ok: true},
		{input: "10000-10100/udp", want: "10000-10100/udp", ok: true},
		{input: "10000:10100/TCP", want: "10000-10100/tcp", ok: true},
		{input: "1234-1234/tcp", want: "1234/tcp", ok: true},
		{input: "0/tcp", ok: false},
		{input: "65536/tcp", ok: false},
		{input: "2000-1000/tcp", ok: false},
		{input: "1-65536/udp", ok: false},
		{input: "1-2-3/tcp", ok: false},
		{input: "443/sctp", ok: false},
		{input: "443", ok: false},
		{input: "abc/tcp", ok: false},
	}
	for _, tt := range tests {
		got, ok := canonicalFirewallSpec(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("canonicalFirewallSpec(%q) = %q, %v; want %q, %v", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestValidFirewallSpec(t *testing.T) {
	for _, spec := range []string{"22/tcp", "443/TCP", "65535/udp", "10000-10100/tcp", "10000:10100/udp"} {
		if !validFirewallSpec(spec) {
			t.Fatalf("expected %q to be valid", spec)
		}
	}
	for _, spec := range []string{"0/tcp", "65536/tcp", "2000-1000/tcp", "443/sctp", "443", "abc/tcp"} {
		if validFirewallSpec(spec) {
			t.Fatalf("expected %q to be invalid", spec)
		}
	}
}

func TestFirewallRuleSpec(t *testing.T) {
	if got := firewallRuleSpec(FirewallRule{Port: 443, Protocol: "TCP"}); got != "443/tcp" {
		t.Fatalf("single-port rule = %q, want 443/tcp", got)
	}
	if got := firewallRuleSpec(FirewallRule{PortRange: "10000-10100", Protocol: "UDP"}); got != "10000-10100/udp" {
		t.Fatalf("range rule = %q, want 10000-10100/udp", got)
	}
}

func TestMieruBindingsProduceCanonicalFirewallSpecs(t *testing.T) {
	bindings := []mieru.PortBinding{
		{Port: 4433, Protocol: "TCP"},
		{PortRange: "20000-20100", Protocol: "UDP"},
	}
	want := []string{"4433/tcp", "20000-20100/udp"}
	got := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		got = append(got, firewallRuleSpec(FirewallRule{
			Port:      binding.Port,
			PortRange: binding.PortRange,
			Protocol:  binding.Protocol,
			Source:    "inbound",
		}))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Mieru firewall specs = %#v, want %#v", got, want)
	}
}

func TestUFWFirewallSpec(t *testing.T) {
	if got := ufwFirewallSpec("10000-10100/tcp"); got != "10000:10100/tcp" {
		t.Fatalf("ufw range spec = %q, want 10000:10100/tcp", got)
	}
	if got := ufwFirewallSpec("443/tcp"); got != "443/tcp" {
		t.Fatalf("ufw single-port spec = %q, want 443/tcp", got)
	}
}
