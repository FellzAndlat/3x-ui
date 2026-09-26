//go:build linux

package systemupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestListAvailableUpdatesCheckupdatesNoUpdates(t *testing.T) {
	dir := t.TempDir()
	checkupdates := filepath.Join(dir, "checkupdates")
	if err := os.WriteFile(checkupdates, []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatalf("write fake checkupdates: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	updates, err := listAvailableUpdates(context.Background(), "pacman")
	if err != nil {
		t.Fatalf("listAvailableUpdates() returned error for checkupdates exit 2: %v", err)
	}
	if len(updates) != 0 {
		t.Fatalf("listAvailableUpdates() = %#v, want no updates", updates)
	}
}
