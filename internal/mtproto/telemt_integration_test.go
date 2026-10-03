package mtproto

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in contract check against an official Telemt release, without relying on
// a public Telegram connection or modifying the host firewall.
func TestOfficialTelemtConfigAndReload(t *testing.T) {
	binary := os.Getenv("TELEMT_TEST_BINARY")
	if binary == "" {
		t.Skip("set TELEMT_TEST_BINARY to an official Telemt binary")
	}
	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)
	port, err := FreeLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	apiPort, err := FreeLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	off := false
	inst := Instance{Listen: "127.0.0.1", Port: port, FakeTLSDomain: "example.com",
		FakeTLSDomains: []string{"example.org"}, TLSMask: &off, TLSEmulation: &off,
		Secrets: []SecretEntry{{Name: "alice", Secret: "0123456789abcdef0123456789abcdef", LimitIP: 2, QuotaBytes: 1073741824, ExpiresUnix: 1893456000}}}
	path := filepath.Join(dir, "telemt.toml")
	if err := writeConfig(path, inst, apiPort, "test-token"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "run", path)
	cmd.Dir = dir
	logs, err := os.Create(filepath.Join(dir, "telemt.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, ok := scrapeStats(apiPort, "test-token"); ok {
			break
		}
		if time.Now().After(deadline) {
			out, _ := os.ReadFile(logs.Name())
			t.Fatalf("Telemt did not accept generated config: %s", out)
		}
		time.Sleep(50 * time.Millisecond)
	}
	inst.Secrets = append(inst.Secrets, SecretEntry{Name: "bob@example.com", Secret: "abcdef0123456789abcdef0123456789"})
	if err := writeConfig(path, inst, apiPort, "test-token"); err != nil {
		t.Fatal(err)
	}
	if !reloadTelemt(apiPort, "test-token") {
		out, _ := os.ReadFile(logs.Name())
		t.Fatalf("Telemt native reload failed: %s", out)
	}
	users, ok := scrapeStats(apiPort, "test-token")
	if !ok {
		t.Fatal("stats unavailable after native reload")
	}
	if _, ok := users[nativeUsername("bob@example.com")]; !ok {
		t.Fatalf("new user missing after native reload: %+v", users)
	}
}
