package service

import (
	"reflect"
	"testing"
)

func TestManagedIPTablesPortRuleArgs(t *testing.T) {
	rule := FirewallRule{Port: 443, Protocol: "tcp"}
	got, ok := managedIPTablesPortRuleArgs(rule)
	if !ok {
		t.Fatal("expected valid iptables rule")
	}
	want := []string{
		"-A", managedIPTablesChain,
		"-p", "tcp",
		"--dport", "443",
		"-m", "comment", "--comment", managedIPTablesRuleComment,
		"-j", "ACCEPT",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestManagedIPTablesPortRangeRuleArgs(t *testing.T) {
	rule := FirewallRule{PortRange: "20000-20100", Protocol: "udp"}
	got, ok := managedIPTablesPortRuleArgs(rule)
	if !ok {
		t.Fatal("expected valid iptables range rule")
	}
	want := []string{
		"-A", managedIPTablesChain,
		"-p", "udp",
		"--dport", "20000:20100",
		"-m", "comment", "--comment", managedIPTablesRuleComment,
		"-j", "ACCEPT",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestManagedIPTablesBaseRulesAreOwned(t *testing.T) {
	for _, args := range managedIPTablesBaseRuleArgs("icmp") {
		found := false
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--comment" && args[i+1] == managedIPTablesRuleComment {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("managed base rule has no ownership comment: %#v", args)
		}
	}
}
