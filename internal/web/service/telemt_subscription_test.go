package service

import (
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
