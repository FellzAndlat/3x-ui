package singbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

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

	p := NewTestProcess(configPath)
	p.setErr(errors.New("stale validation error"))
	if err := p.Validate(context.Background()); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if err := p.GetErr(); err != nil {
		t.Fatalf("successful Validate kept stale error: %v", err)
	}
}

func TestProcessMatchesBinaryRejectsWrongPIDOwner(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process identity verification uses Linux procfs")
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	procPath := "/proc/" + strconv.Itoa(os.Getpid())
	if _, err := os.Readlink(procPath + "/exe"); os.IsPermission(err) || os.IsNotExist(err) {
		t.Skipf("procfs cannot inspect the current PID in this environment: %v", err)
	}
	if !processMatchesBinary(os.Getpid(), exe) {
		t.Fatalf("current PID should match test executable %q", exe)
	}
	if processMatchesBinary(os.Getpid(), filepath.Join(t.TempDir(), "sing-box")) {
		t.Fatal("current PID unexpectedly matched unrelated sing-box path")
	}
}

func TestProcessClearsReusedExternalPID(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("external sing-box discovery uses Linux procfs")
	}

	old, had := os.LookupEnv("XUI_BIN_FOLDER")
	defer func() {
		if had {
			_ = os.Setenv("XUI_BIN_FOLDER", old)
		} else {
			_ = os.Unsetenv("XUI_BIN_FOLDER")
		}
	}()
	if err := os.Setenv("XUI_BIN_FOLDER", t.TempDir()); err != nil {
		t.Fatal(err)
	}

	p := &Process{externalPID: os.Getpid()}
	if p.IsRunning() {
		t.Fatal("unrelated reused PID was reported as running sing-box")
	}
	p.mu.RLock()
	cached := p.externalPID
	p.mu.RUnlock()
	if cached != 0 {
		t.Fatalf("stale external PID was not cleared: %d", cached)
	}
	if got := p.GetUptime(); got != 0 {
		t.Fatalf("stale external PID produced uptime %d", got)
	}
}

func TestProcessGetUptimeStopsWhenManagedProcessDone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses an os.Process handle for the current process")
	}

	old, had := os.LookupEnv("XUI_BIN_FOLDER")
	defer func() {
		if had {
			_ = os.Setenv("XUI_BIN_FOLDER", old)
		} else {
			_ = os.Unsetenv("XUI_BIN_FOLDER")
		}
	}()
	if err := os.Setenv("XUI_BIN_FOLDER", t.TempDir()); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	close(done)
	p := &Process{
		cmd:       &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}},
		done:      done,
		startTime: time.Now().Add(-time.Hour),
		isolated:  true,
	}
	if got := p.GetUptime(); got != 0 {
		t.Fatalf("completed managed process kept reporting uptime %d", got)
	}
}
