package singbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
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

func TestIsolatedProcessNeverAdoptsManagedProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process discovery regression")
	}
	executable, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Skip("sleep binary unavailable")
	}
	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)
	binary := filepath.Join(dir, GetBinaryName())
	if err := os.WriteFile(binary, executable, 0700); err != nil {
		t.Fatal(err)
	}
	production := exec.Command(binary, "30")
	if err := production.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = production.Process.Kill(); _ = production.Wait() }()
	if !NewProcess(filepath.Join(dir, "managed.json")).IsRunning() {
		t.Fatal("managed process was not discovered")
	}
	isolated := NewTestProcess(filepath.Join(dir, "test.json"))
	if isolated.IsRunning() || isolated.GetUptime() != 0 {
		t.Fatal("test process adopted production state")
	}
	if err := isolated.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := production.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("test process stopped production: %v", err)
	}
}
