package youtubeproxy

import (
	"net/http"
	"testing"
	"time"
)

func TestManagedPrepareCommitAndAbort(t *testing.T) {
	Commit(false)
	defer Commit(false)
	dir := t.TempDir()
	options := DefaultOptions()
	first, err := Prepare(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	Commit(true)
	options.Workers = 1
	second, err := Prepare(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("replacement reused live port")
	}
	Abort()
	response, err := http.Get("http://" + Status().Listen + "/status")
	if err != nil {
		t.Fatal("abort killed working proxy", err)
	}
	_ = response.Body.Close()
	_, err = Prepare(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	Commit(true)
	if Status().Listen == "" || !Status().Running {
		t.Fatal("replacement did not commit")
	}
	Commit(false)
	if Status().Running {
		t.Fatal("stop retained proxy")
	}
}
func TestManagedRecoveryAndResourceLimits(t *testing.T) {
	Commit(false)
	defer Commit(false)
	dir := t.TempDir()
	options := DefaultOptions()
	if _, err := Prepare(dir, options); err != nil {
		t.Fatal(err)
	}
	Commit(true)
	managed.Lock()
	_ = managed.active.Server.Close()
	managed.Unlock()
	deadline := time.Now().Add(time.Second)
	for !NeedsRecovery() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !NeedsRecovery() {
		t.Fatal("dead listener was not detected")
	}
	if _, err := Prepare(dir, options); err != nil {
		t.Fatal(err)
	}
	Commit(true)
	if NeedsRecovery() {
		t.Fatal("recovery still reports dead listener")
	}
	options.Workers = 8
	options.BodyMiB = 16
	if options.Validate() == nil {
		t.Fatal("memory budget accepted")
	}
}
func TestFilterDiagnosticReasons(t *testing.T) {
	for _, tc := range []struct {
		body, encoding, reason string
		limit                  int
	}{{"broken", "", "malformed", 1024}, {"x", "br", "unsupported_encoding", 1024}, {"123456789", "", "oversized", 4}} {
		r := response(tc.body, "application/json")
		r.Header.Set("Content-Encoding", tc.encoding)
		changed, reason := filterResponseReason(r, "/youtubei/v1/player", tc.limit)
		_ = r.Body.Close()
		if changed || reason != tc.reason {
			t.Fatalf("reason %s expected %s", reason, tc.reason)
		}
	}
}
