package adblock

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseDomains(t *testing.T) {
	input := `
# AdAway style
0.0.0.0 Ads.Example.com
127.0.0.1 tracker.example.net # inline comment
::1 ipv6.example.org
plain.example.io
||telemetry.example.dev^
! comment
@@||allowed.example.com^
/path/not-a-domain
ads.example.com
`

	got, err := ParseDomains(strings.NewReader(input), []string{"tracker.example.net"})
	if err != nil {
		t.Fatalf("ParseDomains() error = %v", err)
	}
	want := []string{
		"ads.example.com",
		"domain:telemetry.example.dev",
		"ipv6.example.org",
		"plain.example.io",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseDomains() = %#v, want %#v", got, want)
	}
}

func TestParseDomainsRejectsInvalidEntries(t *testing.T) {
	input := "localhost\ninvalid\nexample.com/path\n*.example.com\n"
	got, err := ParseDomains(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("ParseDomains() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ParseDomains() = %#v, want empty", got)
	}
}

func TestHostsAliasesAndRedirects(t *testing.T) {
	got, err := ParseDomains(strings.NewReader("0.0.0.0 one.example two.example # comment\n:: third.example\n127.0.0.1 fourth.example\n192.0.2.1 redirect.example\n0.0.0.0\n127.0.0.2\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fourth.example", "one.example", "third.example", "two.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAllowlistSubdomainsAndSuffixOverlap(t *testing.T) {
	got, err := ParseDomains(strings.NewReader("ads.example.com\nsub.ads.example.com\nnotads.example.com\ndomain:broad.example\nother.example\n"), []string{"ads.example.com", "allowed.broad.example"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"notads.example.com", "other.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSuffixRoundTripAndExceptions(t *testing.T) {
	input := "||example.com^\nwww.example.com\n||sub.example.com^\n@@||safe.example.com^\n||tracker.example^\ntracker.example\nfull:exact.example\n"
	got, err := ParseDomains(strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"domain:sub.example.com", "domain:tracker.example", "exact.example", "tracker.example", "www.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	again, err := ParseDomains(strings.NewReader(strings.Join(got, "\n")), nil)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatalf("round trip: %v, %v", again, err)
	}
}
