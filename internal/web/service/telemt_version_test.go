package service

import "testing"

func TestNormalizeTelemtVersion(t *testing.T) {
	tests := map[string]string{
		"0.7.13":                 "0.7.13",
		"v0.7.13":                "0.7.13",
		"Telemt 0.7.13":          "0.7.13",
		"telemt version v0.7.13": "0.7.13",
		"telemt 0.7.13+build.1":  "0.7.13+build.1",
		"  v0.7.13  ":            "0.7.13",
	}
	for input, want := range tests {
		if got := normalizeTelemtVersion(input); got != want {
			t.Fatalf("normalizeTelemtVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTelemtVersionEqualityDoesNotReportUpdate(t *testing.T) {
	current := "Telemt 0.7.13"
	latest := "v0.7.13"
	if normalizeTelemtVersion(current) != normalizeTelemtVersion(latest) {
		t.Fatalf("equal Telemt versions were normalized differently: %q vs %q", current, latest)
	}
}
