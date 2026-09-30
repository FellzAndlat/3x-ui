package firewall

import (
	"context"
	"testing"
)

func TestDetectFirewalldStateCaseInsensitive(t *testing.T) {
	runner := &fakeCommandRunner{
		paths: map[string]string{
			"firewall-cmd": "/usr/bin/firewall-cmd",
		},
		outputs: map[string]fakeCommandResult{
			"/usr/bin/firewall-cmd --state": {output: []byte("Running\n")},
		},
	}

	got := detect(context.Background(), runner)
	want := Status{Backend: BackendFirewalld, Installed: true, Active: true}
	if got != want {
		t.Fatalf("detect() = %#v, want %#v", got, want)
	}
}
