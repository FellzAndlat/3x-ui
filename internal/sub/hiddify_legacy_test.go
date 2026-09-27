package sub

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

func TestLegacyHiddifySubID(t *testing.T) {
	const id = "768e8bdd-bee3-4442-9006-b26464148aaa"
	aliases := []service.HiddifyLegacySubscriptionAlias{
		{Path: "NvReJ7i2bXWM8kPqdZwz"},
		{Path: "/older-path/"},
	}

	for _, path := range []string{
		"/NvReJ7i2bXWM8kPqdZwz/" + id,
		"/NvReJ7i2bXWM8kPqdZwz/" + id + "/",
		"/older-path/" + id + "/",
	} {
		got, ok := legacyHiddifySubID(path, aliases)
		if !ok || got != id {
			t.Fatalf("legacyHiddifySubID(%q) = %q, %v", path, got, ok)
		}
	}

	for _, path := range []string{
		"/wrong/" + id + "/",
		"/NvReJ7i2bXWM8kPqdZwz/not-a-uuid/",
		"/NvReJ7i2bXWM8kPqdZwz/" + id + "/extra",
	} {
		if got, ok := legacyHiddifySubID(path, aliases); ok {
			t.Fatalf("legacyHiddifySubID(%q) unexpectedly matched %q", path, got)
		}
	}
}

func TestNormalizeRequestHost(t *testing.T) {
	for input, want := range map[string]string{
		"VETROFF.FUN:443": "vetroff.fun",
		"vetroff.fun.":    "vetroff.fun",
		"[::1]:2096":      "::1",
	} {
		if got := normalizeRequestHost(input); got != want {
			t.Fatalf("normalizeRequestHost(%q) = %q, want %q", input, got, want)
		}
	}
}
