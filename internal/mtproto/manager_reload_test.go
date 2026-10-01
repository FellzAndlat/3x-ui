package mtproto

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain re-executes the test binary as a stand-in for Telemt. The historical
// MTG_* env variable names are intentionally kept because process_race_test and
// downstream test tooling already use them.
func TestMain(m *testing.M) {
	if os.Getenv("MTG_FAKE_CHILD") == "1" {
		if f, err := os.OpenFile(os.Getenv("MTG_FAKE_PIDFILE"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
		}
		if exitFile := os.Getenv("MTG_FAKE_EXIT_FILE"); exitFile != "" {
			for {
				if _, err := os.Stat(exitFile); err == nil {
					os.Exit(1)
				} else if !os.IsNotExist(err) {
					os.Exit(2)
				}
				time.Sleep(time.Millisecond)
			}
		}
		select {}
	}
	os.Exit(m.Run())
}

func installFakeMtg(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	payload, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("read test binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, GetBinaryName()), payload, 0o755); err != nil {
		t.Fatalf("install fake Telemt: %v", err)
	}
	pidFile := filepath.Join(binDir, "telemt-pids.txt")
	t.Setenv("XUI_BIN_FOLDER", binDir)
	t.Setenv("MTG_FAKE_CHILD", "1")
	t.Setenv("MTG_FAKE_PIDFILE", pidFile)
	return pidFile
}

func spawnCount(t *testing.T, pidFile string) int {
	t.Helper()
	data, err := os.ReadFile(pidFile)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	return len(strings.Fields(string(data)))
}

func waitSpawnCount(t *testing.T, pidFile string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := spawnCount(t, pidFile)
		if got == want {
			return
		}
		if got > want {
			t.Fatalf("expected %d spawn(s), got %d", want, got)
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d spawn(s), still %d after timeout", want, got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func telemtInst(id int, secrets ...SecretEntry) Instance {
	return Instance{Id: id, Tag: fmt.Sprintf("inbound-%d", id), Listen: "127.0.0.1", Port: 24000 + id, Secrets: secrets, Modes: TelemtModes{TLS: true}, TLSDomain: "example.com", Mask: true, TLSEmulation: true}
}

func TestEnsureActionFor(t *testing.T) {
	cases := []struct {
		running                                      bool
		curStruct, curSecrets, newStruct, newSecrets string
		want                                         ensureAction
	}{
		{false, "s", "a", "s", "a", ensureRestart},
		{true, "s1", "a", "s2", "a", ensureRestart},
		{true, "s", "a", "s", "b", ensureReload},
		{true, "s", "a", "s", "a", ensureNoop},
	}
	for _, tc := range cases {
		if got := ensureActionFor(tc.running, tc.curStruct, tc.curSecrets, tc.newStruct, tc.newSecrets); got != tc.want {
			t.Fatalf("ensureActionFor=%d want=%d", got, tc.want)
		}
	}
}

func TestEnsureNoopKeepsProcess(t *testing.T) {
	pidFile := installFakeMtg(t)
	mgr := &Manager{procs: map[int]*managed{}, swept: true}
	inst := telemtInst(3, SecretEntry{Name: "alice", Secret: "0123456789abcdef0123456789abcdef"})
	if err := mgr.Ensure(inst); err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	waitSpawnCount(t, pidFile, 1)
	if err := mgr.Ensure(inst); err != nil {
		t.Fatalf("repeat ensure: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := spawnCount(t, pidFile); got != 1 {
		t.Fatalf("unchanged instance spawned %d processes", got)
	}
	mgr.StopAll()
}
