package sudoku

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestManagerRejectsInvalidConfigAndRollsBackFailedStartup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	binDir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", binDir)
	script := `#!/bin/sh
if [ "$1" = "-test" ]; then
 case "$(cat "$3")" in *invalid*) exit 1;; esac
 exit 0
fi
case "$(cat "$2")" in *runtime-failure*) exit 1;; esac
exec sleep 60
`
	if err := os.WriteFile(GetBinaryPath(binDir), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{procs: make(map[int]*managed), lastErr: make(map[int]string)}
	t.Cleanup(manager.StopAll)
	instance := Instance{ID: 1, Tag: "sudoku-test", Config: Config{Mode: "server", LocalPort: 2443, AEAD: "none"}}
	manager.Reconcile(context.Background(), []Instance{instance})
	original := manager.procs[1]
	if original == nil || !original.proc.IsRunning() {
		t.Fatal("initial process did not start")
	}
	contents, err := os.ReadFile(configPath(binDir, 1))
	if err != nil {
		t.Fatal(err)
	}
	instance.Config.AEAD = "invalid"
	manager.Reconcile(context.Background(), []Instance{instance})
	if manager.procs[1] != original || !original.proc.IsRunning() || manager.lastErr[1] == "" {
		t.Fatal("invalid config disrupted healthy process or lost error")
	}
	instance.Config.AEAD = "runtime-failure"
	manager.Reconcile(context.Background(), []Instance{instance})
	if manager.procs[1] != original || !original.proc.IsRunning() {
		t.Fatal("failed startup did not restore previous process")
	}
	restored, err := os.ReadFile(configPath(binDir, 1))
	if err != nil || string(restored) != string(contents) {
		t.Fatalf("rollback config was lost: %s, %v", restored, err)
	}
	instance.Config.AEAD = "none"
	manager.Reconcile(context.Background(), []Instance{instance})
	if manager.lastErr[1] != "" {
		t.Fatal("successful reconciliation retained a stale error")
	}
	manager.Reconcile(context.Background(), nil)
	if len(manager.procs) != 0 || original.proc.IsRunning() {
		t.Fatal("removed inbound is still running")
	}
}

func TestManagerRemoveCleansCredentialState(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", binDir)
	const id = 42
	if err := WriteMasterKey(binDir, id, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err := WriteClientRoster(binDir, id, []string{"alice@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := BeginCredentialRotation(binDir, id); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{procs: make(map[int]*managed), lastErr: make(map[int]string)}
	manager.Remove(id)
	for _, path := range []string{keyPath(binDir, id), clientRosterPath(binDir, id), credentialRotationPath(binDir, id)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("credential state still exists: %s, %v", path, err)
		}
	}
}
