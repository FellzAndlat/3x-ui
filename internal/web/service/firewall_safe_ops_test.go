package service

import (
	"testing"
	"unicode/utf8"
)

func TestAppendFrozenInboundRules(t *testing.T) {
	desired := []FirewallRule{{Port: 443, Protocol: "tcp", Source: "panel"}}
	managed := []FirewallRule{
		{Port: 443, Protocol: "tcp", Source: "panel"},
		{Port: 8443, Protocol: "tcp", Source: "inbound", Label: "VLESS"},
		{Port: 53, Protocol: "udp", Source: "manual"},
	}
	got := appendFrozenInboundRules(desired, managed)
	if len(got) != 2 {
		t.Fatalf("len = %d, want panel + frozen inbound", len(got))
	}
	if got[0].Port != 443 || got[1].Port != 8443 || got[1].Source != "inbound" {
		t.Fatalf("unexpected frozen rules: %#v", got)
	}
}

func TestNormalizeFirewallLabelUnicode(t *testing.T) {
	label := "  Прокси для мониторинга  "
	if got := normalizeFirewallLabelUnicode(label); got != "Прокси для мониторинга" {
		t.Fatalf("label = %q", got)
	}

	long := ""
	for range 130 {
		long += "я"
	}
	got := normalizeFirewallLabelUnicode(long)
	if len(got) > 120 {
		t.Fatalf("byte length = %d, want <= 120", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncated label is not valid UTF-8")
	}
	if len([]rune(got)) != 60 {
		t.Fatalf("rune count = %d, want 60 Cyrillic runes in 120 bytes", len([]rune(got)))
	}
}
