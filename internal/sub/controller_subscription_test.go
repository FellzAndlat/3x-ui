package sub

import "testing"

func TestBuildRawSubscriptionBodyNormalizesAcrossEntries(t *testing.T) {
	got := buildRawSubscriptionBody([]string{
		"vless://same",
		"vless://same",
		"mieru://QUJDRA==\nmierus://user:pass@example.com?port=443&protocol=TCP#Mieru",
		"mierus://user:pass@example.com?port=443&protocol=TCP#Mieru",
		"trojan://other",
	})

	want := "vless://same\n" +
		"mierus://user:pass@example.com?port=443&protocol=TCP#Mieru\n" +
		"trojan://other\n"
	if got != want {
		t.Fatalf("normalized raw subscription body = %q, want %q", got, want)
	}
}
