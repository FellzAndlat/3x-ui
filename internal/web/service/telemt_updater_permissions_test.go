package service

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTelemtUpdaterRepairsNonExecutableInstallation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Telemt updater is a Linux shell script")
	}
	path := filepath.Join(t.TempDir(), "telemt-update.sh")
	if err := os.WriteFile(path, []byte("#!/bin/bash\necho latest=1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The installed script may have lost its executable bit, but checking the
	// version must still work and the next update repairs the file mode.
	out, err := telemtUpdaterCommand(context.Background(), path, "--check").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "latest=1.2.3") {
		t.Fatalf("check a non-executable updater: %s, %v", out, err)
	}
	if err := repairTelemtUpdaterMode(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("updater mode = %v, want 0755", info.Mode().Perm())
	}
}
