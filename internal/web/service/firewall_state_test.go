package service

import "testing"

func TestShouldRestoreManagedNativeFirewall(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		active  bool
		desired bool
		want    bool
	}{
		{name: "restore nftables", backend: "nftables", desired: true, want: true},
		{name: "restore iptables", backend: "iptables", desired: true, want: true},
		{name: "active native stays untouched", backend: "nftables", active: true, desired: true},
		{name: "disabled native stays disabled", backend: "iptables", desired: false},
		{name: "do not restart ufw", backend: "ufw", desired: true},
		{name: "do not restart firewalld", backend: "firewalld", desired: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRestoreManagedNativeFirewall(tt.backend, tt.active, tt.desired); got != tt.want {
				t.Fatalf("shouldRestoreManagedNativeFirewall(%q, %v, %v) = %v, want %v", tt.backend, tt.active, tt.desired, got, tt.want)
			}
		})
	}
}
