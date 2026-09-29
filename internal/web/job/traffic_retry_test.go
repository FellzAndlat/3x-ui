package job

import (
	"errors"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func TestPendingTrafficBatchRetainsFailedCommitAndClearsAfterRetry(t *testing.T) {
	inbounds := []*xray.Traffic{{Tag: "inbound-1", IsInbound: true, Up: 11, Down: 22}}
	clients := []*xray.ClientTraffic{{Email: "user@example.com", Up: 33, Down: 44}}
	batch := pendingTrafficBatch{}
	batch.remember(inbounds, clients)

	sentinel := errors.New("temporary database failure")
	calls := 0
	commit := func(gotInbounds []*xray.Traffic, gotClients []*xray.ClientTraffic) (bool, bool, error) {
		calls++
		if len(gotInbounds) != 1 || gotInbounds[0] != inbounds[0] {
			t.Fatalf("unexpected inbound retry batch: %#v", gotInbounds)
		}
		if len(gotClients) != 1 || gotClients[0] != clients[0] {
			t.Fatalf("unexpected client retry batch: %#v", gotClients)
		}
		if calls == 1 {
			return false, false, sentinel
		}
		return true, true, nil
	}

	needRestart, clientsDisabled, err := batch.flush(commit)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected first commit to fail with sentinel, got %v", err)
	}
	if needRestart || clientsDisabled {
		t.Fatalf("unexpected flags on failed commit: restart=%v disabled=%v", needRestart, clientsDisabled)
	}
	if !batch.hasData() {
		t.Fatal("failed traffic batch was discarded")
	}

	needRestart, clientsDisabled, err = batch.flush(commit)
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if !needRestart || !clientsDisabled {
		t.Fatalf("commit flags were not propagated: restart=%v disabled=%v", needRestart, clientsDisabled)
	}
	if batch.hasData() {
		t.Fatal("successful retry did not clear pending traffic")
	}
	if calls != 2 {
		t.Fatalf("expected exactly two commit attempts, got %d", calls)
	}
}

func TestPendingTrafficBatchEmptyFlushDoesNotCallCommit(t *testing.T) {
	batch := pendingTrafficBatch{}
	called := false
	_, _, err := batch.flush(func([]*xray.Traffic, []*xray.ClientTraffic) (bool, bool, error) {
		called = true
		return false, false, nil
	})
	if err != nil {
		t.Fatalf("empty flush returned error: %v", err)
	}
	if called {
		t.Fatal("empty batch invoked commit callback")
	}
}
