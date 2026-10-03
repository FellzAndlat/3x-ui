package service

import (
	"reflect"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestNormalizeSudokuSettingsDefaults(t *testing.T) {
	settings, err := normalizeSudokuSettings(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	if settings.AEAD != "chacha20-poly1305" {
		t.Fatalf("AEAD = %q", settings.AEAD)
	}
	if settings.SuspiciousAction != "silent" {
		t.Fatalf("SuspiciousAction = %q", settings.SuspiciousAction)
	}
	if settings.PaddingMin != 5 || settings.PaddingMax != 15 {
		t.Fatalf("padding = %d..%d", settings.PaddingMin, settings.PaddingMax)
	}
	if settings.ASCII != "prefer_entropy" {
		t.Fatalf("ASCII = %q", settings.ASCII)
	}
	if !settings.EnablePureDownlink {
		t.Fatal("EnablePureDownlink should default to true")
	}
	if settings.Multiplex != "off" || settings.HTTPMask.Multiplex != "off" {
		t.Fatalf("multiplex defaults = %q/%q", settings.Multiplex, settings.HTTPMask.Multiplex)
	}
	if settings.HTTPMask.Mode != "auto" {
		t.Fatalf("HTTP mask mode = %q, want auto", settings.HTTPMask.Mode)
	}
}

func TestNormalizeSudokuSettingsRepairsInvalidRangesAndFallback(t *testing.T) {
	settings, err := normalizeSudokuSettings(`{
		"suspiciousAction":"fallback",
		"paddingMin":101,
		"paddingMax":1,
		"httpmask":{"mode":"","multiplex":"stream"}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if settings.SuspiciousAction != "silent" {
		t.Fatalf("fallback without address must become silent, got %q", settings.SuspiciousAction)
	}
	if settings.PaddingMin != 5 || settings.PaddingMax != 15 {
		t.Fatalf("invalid padding was not repaired: %d..%d", settings.PaddingMin, settings.PaddingMax)
	}
	if settings.HTTPMask.Mode != "auto" {
		t.Fatalf("empty HTTP mask mode = %q, want auto", settings.HTTPMask.Mode)
	}
	if settings.Multiplex != "stream" || settings.HTTPMask.Multiplex != "stream" {
		t.Fatalf("HTTP mask multiplex should win and stay synchronized: %q/%q", settings.Multiplex, settings.HTTPMask.Multiplex)
	}
}

func TestActiveSudokuClientRoster(t *testing.T) {
	clients := []model.Client{
		{Email: " Bob@example.com ", Enable: true},
		{Email: "ALICE@example.com", Enable: true},
		{Email: "alice@EXAMPLE.com", Enable: true},
		{Email: "disabled@example.com", Enable: false},
		{Email: "  ", Enable: true},
	}
	want := []string{"alice@example.com", "bob@example.com"}
	if got := activeSudokuClientRoster(clients); !reflect.DeepEqual(got, want) {
		t.Fatalf("activeSudokuClientRoster() = %#v, want %#v", got, want)
	}
}

func TestSudokuRosterSetNormalizesIdentity(t *testing.T) {
	set := sudokuRosterSet([]string{" ALICE@example.com ", "alice@EXAMPLE.com", ""})
	if len(set) != 1 {
		t.Fatalf("set size = %d, want 1", len(set))
	}
	if _, ok := set["alice@example.com"]; !ok {
		t.Fatal("normalized identity missing")
	}
}
