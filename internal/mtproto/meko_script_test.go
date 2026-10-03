package mtproto

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Exercise the bundled helper with a fake firewall. Concurrent invocations
// must never interleave chain rebuilds; no host firewall rules are touched.
func TestMekoHelperSerializesRuleUpdates(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux firewall helper")
	}
	for _, tool := range []string{"bash", "flock"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is unavailable", tool)
		}
	}
	dir := t.TempDir()
	data, err := os.ReadFile("../Telemt/telemt-meko-fix.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(data), "/run/lock/x-ui-telemt-meko.lock", filepath.Join(dir, "lock"))
	script = strings.ReplaceAll(script, "/etc/x-ui/telemt-meko-fix.env", filepath.Join(dir, "state.env"))
	helper := filepath.Join(dir, "helper.sh")
	if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	mock := `#!/usr/bin/env bash
set -eu
if ! mkdir "$MEKO_TEST_DIR/busy" 2>/dev/null; then
    echo overlap >> "$MEKO_TEST_DIR/errors"
    exit 1
fi
trap 'rmdir "$MEKO_TEST_DIR/busy"' EXIT
printf '%s\n' "$*" >> "$MEKO_TEST_DIR/calls"
sleep 0.005
for arg in "$@"; do
    [[ "$arg" != "-C" ]] || exit 1
done
`
	if err := os.WriteFile(filepath.Join(dir, "iptables"), []byte(mock), 0o700); err != nil {
		t.Fatal(err)
	}
	// Never invoke a real host firewall, even when nft is installed.
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"MEKO_TEST_DIR="+dir, "TELEMT_MEKO_ENABLED=1", "TELEMT_PORTS=443,8443,443", "TELEMT_MEKO_RATE=54/minute", "TELEMT_MEKO_BURST=1")
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			cmd := exec.Command("bash", helper, "apply")
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("MEKO apply failed: %v: %s", err, out)
			}
		})
	}
	wg.Wait()
	if data, err := os.ReadFile(filepath.Join(dir, "errors")); err == nil {
		t.Fatalf("firewall updates overlapped: %s", data)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-w 2", "--set-xmark 0x400/0x400", "--hashlimit-upto 54/minute", "--hashlimit-burst 1", "--reject-with tcp-reset"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("MEKO V3 lost %s", want)
		}
	}
	if strings.Count(string(calls), "--hashlimit-name tm_443 ") != 2 {
		t.Fatal("each update must deduplicate the discovered port set")
	}
}

func TestMekoNftBatchAndStatus(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux helper")
	}
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock unavailable")
	}
	dir := t.TempDir()
	data, err := os.ReadFile("../Telemt/telemt-meko-fix.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(data), "/run/lock/x-ui-telemt-meko.lock", filepath.Join(dir, "lock"))
	script = strings.ReplaceAll(script, "/etc/x-ui/telemt-meko-fix.env", filepath.Join(dir, "state.env"))
	helper := filepath.Join(dir, "helper.sh")
	if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	mock := `#!/usr/bin/env bash
set -eu
case "$*" in
 '-f -') cat > "$MEKO_TEST_DIR/batch" ;;
 'list chain inet xui_telemt_meko input') echo 'reject with tcp reset' ;;
 '-n list set inet xui_telemt_meko ports') printf 'elements = { 443,\n 8443 }\n' ;;
 'delete table inet xui_telemt_meko') touch "$MEKO_TEST_DIR/deleted" ;;
 *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(mock), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "iptables"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	run := func(action, enabled string) string {
		t.Helper()
		cmd := exec.Command("bash", helper, action)
		cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "MEKO_TEST_DIR="+dir, "TELEMT_PORTS=8443,443,443", "TELEMT_MEKO_ENABLED="+enabled)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v %s", action, err, out)
		}
		return string(out)
	}
	run("apply", "1")
	batch, err := os.ReadFile(filepath.Join(dir, "batch"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"flush table inet xui_telemt_meko", "ports { 443,8443 }", "@th,320,32 0x04020000", "ip saddr timeout 60s", "ip6 saddr timeout 60s", "54/minute burst 1 packets", "counter return"} {
		if !strings.Contains(string(batch), want) {
			t.Errorf("missing %s: %s", want, batch)
		}
	}
	if strings.Count(string(batch), "reject with tcp reset") != 4 {
		t.Fatal("expected one IPv4 and IPv6 limiter per port")
	}
	status := run("status", "1")
	if !strings.Contains(status, "installed=true") || !strings.Contains(status, "applied_ports=443,8443") {
		t.Fatalf("incorrect status: %s", status)
	}
	run("apply", "0")
	if _, err := os.Stat(filepath.Join(dir, "deleted")); err != nil {
		t.Fatal("disable did not remove owned nft table")
	}
}
