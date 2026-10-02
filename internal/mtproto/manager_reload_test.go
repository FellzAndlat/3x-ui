package mtproto

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("TELEMT_FAKE_CHILD") == "1" {
		if f, err := os.OpenFile(os.Getenv("TELEMT_FAKE_PIDFILE"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
		}
		if exitFile := os.Getenv("TELEMT_FAKE_EXIT_FILE"); exitFile != "" {
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

func installFakeTelemt(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, GetBinaryName()), payload, 0o755); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(binDir, "telemt-pids.txt")
	t.Setenv("XUI_BIN_FOLDER", binDir)
	t.Setenv("TELEMT_FAKE_CHILD", "1")
	t.Setenv("TELEMT_FAKE_PIDFILE", pidFile)
	return pidFile
}

func spawnCount(t *testing.T, p string) int {
	t.Helper()
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(string(b)))
}

func waitSpawnCount(t *testing.T, p string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if got := spawnCount(t, p); got == want {
			return
		} else if got > want {
			t.Fatalf("spawns=%d want=%d", got, want)
		}
		if time.Now().After(deadline) {
			t.Fatalf("spawn timeout: got %d want %d", spawnCount(t, p), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func telemtInst(id int, secrets ...SecretEntry) Instance {
	return Instance{Id: id, Tag: fmt.Sprintf("inbound-%d", id), Listen: "127.0.0.1", Port: 24000 + id, FakeTLSDomain: "example.com", Secrets: secrets}
}

func TestEnsureActionFor(t *testing.T) {
	if ensureActionFor(false, "s", "a", "s", "a") != ensureRestart {
		t.Fatal("dead process must restart")
	}
	if ensureActionFor(true, "s1", "a", "s2", "a") != ensureRestart {
		t.Fatal("structural change must restart")
	}
	if ensureActionFor(true, "s", "a", "s", "b") != ensureReload {
		t.Fatal("user change must reload")
	}
	if ensureActionFor(true, "s", "a", "s", "a") != ensureNoop {
		t.Fatal("unchanged must be noop")
	}
}

func TestReloadTelemtWaitsForRuntimeActivation(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sesame" {
			t.Errorf("bad auth header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/system/reload":
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprint(w, `{"ok":true,"data":{"reload_id":7,"state":"accepted"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/system/reload/7":
			state := "activating"
			if polls.Add(1) >= 2 {
				state = "succeeded"
			}
			_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"reload_id":7,"state":%q}}`, state)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if !reloadTelemt(serverPort(t, srv), "sesame") {
		t.Fatal("reload should succeed only after terminal succeeded state")
	}
	if polls.Load() < 2 {
		t.Fatalf("expected reload status polling, polls=%d", polls.Load())
	}
}

func TestReloadTelemtRejectsFailedActivation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/v1/system/reload" {
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprint(w, `{"ok":true,"data":{"reload_id":9,"state":"accepted"}}`)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/system/reload/9" {
			_, _ = fmt.Fprint(w, `{"ok":true,"data":{"reload_id":9,"state":"failed"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	if reloadTelemt(serverPort(t, srv), "sesame") {
		t.Fatal("failed runtime activation must not be reported as successful reload")
	}
}

func TestEnsureHotReloadKeepsProcess(t *testing.T) {
	pidFile := installFakeTelemt(t)
	mgr := &Manager{procs: map[int]*managed{}, swept: true}
	inst := telemtInst(1, SecretEntry{Name: "alice", Secret: "0123456789abcdef0123456789abcdef"})
	if err := mgr.Ensure(inst); err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	waitSpawnCount(t, pidFile, 1)
	orig := mgr.procs[1].proc
	token := mgr.procs[1].apiToken
	reloaded := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/v1/system/reload" {
			reloaded <- struct{}{}
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprint(w, `{"ok":true,"data":{"reload_id":11,"state":"accepted"}}`)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/system/reload/11" {
			_, _ = fmt.Fprint(w, `{"ok":true,"data":{"reload_id":11,"state":"succeeded"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	mgr.procs[1].apiPort = serverPort(t, srv)
	changed := telemtInst(1,
		SecretEntry{Name: "alice", Secret: "0123456789abcdef0123456789abcdef"},
		SecretEntry{Name: "bob", Secret: "abcdef0123456789abcdef0123456789"})
	if err := mgr.Ensure(changed); err != nil {
		t.Fatalf("reload ensure: %v", err)
	}
	select {
	case <-reloaded:
	case <-time.After(time.Second):
		t.Fatal("expected Telemt reload")
	}
	if spawnCount(t, pidFile) != 1 {
		t.Fatal("reload must keep process")
	}
	if mgr.procs[1].proc != orig {
		t.Fatal("process changed during reload")
	}
	cfg, err := os.ReadFile(configPathForID(1))
	if err != nil {
		t.Fatal(err)
	}
	s := string(cfg)
	if !strings.Contains(s, `"bob" = "abcdef0123456789abcdef0123456789"`) {
		t.Fatalf("new user missing:\n%s", s)
	}
	if !strings.Contains(s, "[server.api]") || !strings.Contains(s, "Bearer "+token) {
		t.Fatalf("API token must be reused:\n%s", s)
	}
	mgr.StopAll()
}

func TestEnsureReloadFallbackRestarts(t *testing.T) {
	pidFile := installFakeTelemt(t)
	mgr := &Manager{procs: map[int]*managed{}, swept: true}
	if err := mgr.Ensure(telemtInst(2, SecretEntry{Name: "alice", Secret: "0123456789abcdef0123456789abcdef"})); err != nil {
		t.Fatal(err)
	}
	waitSpawnCount(t, pidFile, 1)
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	mgr.procs[2].apiPort = serverPort(t, srv)
	if err := mgr.Ensure(telemtInst(2, SecretEntry{Name: "carol", Secret: "abcdef0123456789abcdef0123456789"})); err != nil {
		t.Fatal(err)
	}
	waitSpawnCount(t, pidFile, 2)
	mgr.StopAll()
}

func TestRemoveStaleConfigs(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", binDir)
	if err := os.MkdirAll(configDir(), 0o750); err != nil {
		t.Fatal(err)
	}
	stale := configPathForID(1)
	keep := configPathForID(2)
	foreign := filepath.Join(configDir(), "telemt-bad.toml")
	for _, path := range []string{stale, keep, foreign} {
		if err := os.WriteFile(path, []byte("test"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if got := removeStaleConfigs(map[int]struct{}{2: {}}); got != 1 {
		t.Fatalf("removed=%d want=1", got)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale config still exists: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("wanted config was removed: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("unrecognized config must be left untouched: %v", err)
	}
}
