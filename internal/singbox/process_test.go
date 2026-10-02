package singbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProcessSupportsNativeAPI(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"Unknown", false},
		{"", false},
		{"1.13.9", false},
		{"1.14.0", true},
		{"1.14.1", true},
		{"2.0.0", true},
	}
	for _, tc := range cases {
		p := &Process{version: tc.version}
		if got := p.SupportsNativeAPI(); got != tc.want {
			t.Fatalf("version %q: got %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestManagedPathsAreAbsolute(t *testing.T) {
	old, had := os.LookupEnv("XUI_BIN_FOLDER")
	defer func() {
		if had {
			_ = os.Setenv("XUI_BIN_FOLDER", old)
		} else {
			_ = os.Unsetenv("XUI_BIN_FOLDER")
		}
	}()
	_ = os.Setenv("XUI_BIN_FOLDER", "bin")
	if !filepath.IsAbs(GetBinaryPath()) || !filepath.IsAbs(GetConfigPath()) {
		t.Fatalf("managed sing-box paths must be absolute: binary=%q config=%q", GetBinaryPath(), GetConfigPath())
	}
}

func TestProcessGetUptimeWithoutStartTime(t *testing.T) {
	p := &Process{}
	if got := p.GetUptime(); got != 0 {
		t.Fatalf("expected zero uptime for a process without start metadata, got %d", got)
	}
}

func TestProcessValidateClearsStaleError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable fixture uses a POSIX shell")
	}

	old, had := os.LookupEnv("XUI_BIN_FOLDER")
	defer func() {
		if had {
			_ = os.Setenv("XUI_BIN_FOLDER", old)
		} else {
			_ = os.Unsetenv("XUI_BIN_FOLDER")
		}
	}()
	binDir := t.TempDir()
	if err := os.Setenv("XUI_BIN_FOLDER", binDir); err != nil {
		t.Fatal(err)
	}

	binary := GetBinaryPath()
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := GetConfigPath()
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := NewProcess(configPath)
	p.setErr(errors.New("stale validation error"))
	if err := p.Validate(context.Background()); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if err := p.GetErr(); err != nil {
		t.Fatalf("successful Validate kept stale error: %v", err)
	}
}
