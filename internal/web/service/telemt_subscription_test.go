package service

import (
	"fmt"
	"strings"
	"testing"
)

func TestTelemtSubscriptionUsernameStable(t *testing.T) {
	const subID = "subscription-123"

	username := telemtSubscriptionUsername(subID)
	trimmedUsername := telemtSubscriptionUsername("  " + subID + "  ")
	if username != trimmedUsername {
		t.Fatalf("expected surrounding whitespace to be ignored: %q != %q", username, trimmedUsername)
	}
	if !strings.HasPrefix(username, telemtSubscriptionUserPrefix) {
		t.Fatalf("expected %q prefix, got %q", telemtSubscriptionUserPrefix, username)
	}
	if got, want := len(username), len(telemtSubscriptionUserPrefix)+24; got != want {
		t.Fatalf("unexpected username length: got %d, want %d", got, want)
	}
	if strings.Contains(username, subID) {
		t.Fatalf("subscription id leaked into Telemt username: %q", username)
	}
	if !isTelemtSubscriptionUsername(username) {
		t.Fatalf("generated username was not recognized: %q", username)
	}

	other := telemtSubscriptionUsername("subscription-124")
	if username == other {
		t.Fatalf("different subscription ids produced the same username: %q", username)
	}
}

func TestIsTelemtSubscriptionUsernameRejectsMalformed(t *testing.T) {
	valid := telemtSubscriptionUsername("subscription-123")
	tests := []string{
		"",
		telemtSubscriptionUserPrefix,
		"other_0123456789abcdef01234567",
		telemtSubscriptionUserPrefix + "0123456789abcdef0123456z",
		valid + "00",
	}

	for _, value := range tests {
		if isTelemtSubscriptionUsername(value) {
			t.Errorf("expected malformed subscription username to be rejected: %q", value)
		}
	}
}

func TestTelemtSubscriptionUsernamesFiltersAndSorts(t *testing.T) {
	first := telemtSubscriptionUsername("subscription-a")
	second := telemtSubscriptionUsername("subscription-b")
	users := map[string]string{
		"xui":   "admin-secret",
		second:  "second-secret",
		"alice": "manual-secret",
		first:   "first-secret",
	}

	got := telemtSubscriptionUsernames(users)
	if len(got) != 2 {
		t.Fatalf("expected 2 subscription users, got %d: %#v", len(got), got)
	}
	if got[0] > got[1] {
		t.Fatalf("expected deterministic sorted usernames, got %#v", got)
	}
	if got[0] != first && got[1] != first {
		t.Fatalf("first subscription user missing from %#v", got)
	}
	if got[0] != second && got[1] != second {
		t.Fatalf("second subscription user missing from %#v", got)
	}
}

func TestRemoveTelemtSubscriptionUsersFromTOML(t *testing.T) {
	first := telemtSubscriptionUsername("subscription-a")
	second := telemtSubscriptionUsername("subscription-b")
	outsideUsers := telemtSubscriptionUsername("not-an-access-user")
	original := []byte(fmt.Sprintf(`[server]
port = 443

[access.users]
xui = "admin-secret"
%s = "first-secret"
"%s" = "second-secret"
alice = "manual-secret"

[custom]
%s = "must-stay"
`, first, second, outsideUsers))

	updated, removed, err := removeTelemtSubscriptionUsersFromTOML(original)
	if err != nil {
		t.Fatalf("remove subscription users: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 removed subscription users, got %d", removed)
	}
	text := string(updated)
	if strings.Contains(text, first) || strings.Contains(text, second) {
		t.Fatalf("subscription users were left in access.users:\n%s", text)
	}
	for _, preserved := range []string{"xui = \"admin-secret\"", "alice = \"manual-secret\"", outsideUsers + " = \"must-stay\""} {
		if !strings.Contains(text, preserved) {
			t.Fatalf("expected unrelated TOML entry %q to be preserved:\n%s", preserved, text)
		}
	}
}
