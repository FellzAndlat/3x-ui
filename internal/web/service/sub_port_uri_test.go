package service

import "testing"

func TestMoveSubscriptionURIPort(t *testing.T) {
	cases := []struct {
		uri  string
		old  int
		next int
		want string
	}{
		{"https://sub.example.com:2096/sub/?format=json", 2096, 8443, "https://sub.example.com:8443/sub/?format=json"},
		{"https://sub.example.com:2096/json/", 2096, 443, "https://sub.example.com/json/"},
		{"http://[::1]:2096/clash/", 2096, 8080, "http://[::1]:8080/clash/"},
		{"https://proxy.example.com/sub/", 2096, 8443, "https://proxy.example.com/sub/"},
		{"https://proxy.example.com:443/sub/", 2096, 8443, "https://proxy.example.com:443/sub/"},
	}
	for _, tc := range cases {
		if got := moveSubscriptionURIPort(tc.uri, tc.old, tc.next); got != tc.want {
			t.Errorf("moveSubscriptionURIPort(%q, %d, %d) = %q, want %q", tc.uri, tc.old, tc.next, got, tc.want)
		}
	}
}
