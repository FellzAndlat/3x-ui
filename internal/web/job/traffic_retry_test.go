package job

import (
	"errors"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func TestAccumulateTrafficDelta(t *testing.T) {
	tests := []struct {
		name    string
		current int64
		delta   int64
		want    int64
	}{
		{name: "normal", current: 10, delta: 20, want: 30},
		{name: "negative runtime delta ignored", current: 10, delta: -7, want: 10},
		{name: "negative current normalized", current: -5, delta: 7, want: 7},
		{name: "saturates before overflow", current: database.TrafficMax - 2, delta: 10, want: database.TrafficMax},
		{name: "already saturated", current: database.TrafficMax, delta: 10, want: database.TrafficMax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := accumulateTrafficDelta(tt.current, tt.delta); got != tt.want {
				t.Fatalf("accumulateTrafficDelta(%d, %d) = %d, want %d", tt.current, tt.delta, got, tt.want)
			}
		})
	}
}

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

func TestPendingTrafficBatchAppendKeepsOlderAndFinalSnapshots(t *testing.T) {
	oldInbound := &xray.Traffic{Tag: "tuic-1", IsInbound: true, Up: 10, Down: 20}
	oldClient := &xray.ClientTraffic{Email: "alice@example.com", Up: 3, Down: 4}
	finalInbound := &xray.Traffic{Tag: "tuic-1", IsInbound: true, Up: 30, Down: 40}
	finalClient := &xray.ClientTraffic{Email: "alice@example.com", Up: 5, Down: 6}

	batch := pendingTrafficBatch{}
	batch.remember([]*xray.Traffic{oldInbound}, []*xray.ClientTraffic{oldClient})
	batch.appendBatch([]*xray.Traffic{finalInbound}, []*xray.ClientTraffic{finalClient})

	calls := 0
	_, _, err := batch.flush(func(inbounds []*xray.Traffic, clients []*xray.ClientTraffic) (bool, bool, error) {
		calls++
		if len(inbounds) != 2 || inbounds[0] != oldInbound || inbounds[1] != finalInbound {
			t.Fatalf("combined inbound batch = %#v", inbounds)
		}
		if len(clients) != 2 || clients[0] != oldClient || clients[1] != finalClient {
			t.Fatalf("combined client batch = %#v", clients)
		}
		return false, false, nil
	})
	if err != nil {
		t.Fatalf("flush combined batch: %v", err)
	}
	if calls != 1 {
		t.Fatalf("commit calls = %d, want 1", calls)
	}
	if batch.hasData() {
		t.Fatal("combined batch not cleared after successful commit")
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
