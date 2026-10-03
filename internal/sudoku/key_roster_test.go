package sudoku

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestSudokuKeyValidation(t *testing.T) {
	public64 := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	private128 := public64 + public64

	if !ValidPublicKey(public64) {
		t.Fatal("64-hex public key must be valid")
	}
	if ValidPublicKey(private128) {
		t.Fatal("128-hex private key must not be accepted as a public key")
	}
	if !ValidPrivateKey(public64) {
		t.Fatal("64-hex private key must be valid")
	}
	if !ValidPrivateKey(private128) {
		t.Fatal("128-hex split/private key must be valid")
	}
	if !ValidPrivateKey("  " + private128 + "\n") {
		t.Fatal("private key validation should trim whitespace")
	}
	if ValidPrivateKey(public64[:63]) {
		t.Fatal("short private key must be rejected")
	}
	if ValidPrivateKey(public64[:63] + "z") {
		t.Fatal("non-hex private key must be rejected")
	}
}

func TestFindLabeledValue(t *testing.T) {
	output := "noise\r\n  Master Public Key: abc123  \r\nmore noise\n"
	if got := findLabeledValue(output, "Master Public Key:"); got != "abc123" {
		t.Fatalf("findLabeledValue() = %q, want %q", got, "abc123")
	}
}

func TestClientRosterRoundTripNormalizesAndSorts(t *testing.T) {
	binDir := t.TempDir()
	want := []string{"alice@example.com", "bob@example.com"}
	input := []string{
		" Bob@example.com ",
		"ALICE@example.com",
		"alice@EXAMPLE.com",
		"",
		"bob@example.com",
	}

	if err := WriteClientRoster(binDir, 42, input); err != nil {
		t.Fatal(err)
	}
	got, err := ReadClientRoster(binDir, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadClientRoster() = %#v, want %#v", got, want)
	}
}

func TestClientRosterMissing(t *testing.T) {
	_, err := ReadClientRoster(t.TempDir(), 99)
	if err == nil {
		t.Fatal("expected missing roster error")
	}
	if !ClientRosterMissing(err) {
		t.Fatalf("ClientRosterMissing(%v) = false", err)
	}
	if ClientRosterMissing(errors.New("other")) {
		t.Fatal("unrelated errors must not be reported as a missing roster")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing roster error = %v, want os.ErrNotExist", err)
	}
}
