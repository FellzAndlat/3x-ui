package mtproto

import "testing"

func TestMergeCounterSnapshotsPreservesTemporarilyMissingUser(t *testing.T) {
	previous := map[string]clientCounters{
		"alice": {up: 100, down: 200},
	}

	next, deltas, online := mergeCounterSnapshots(previous, map[string]statsUser{})
	if len(deltas) != 0 || len(online) != 0 {
		t.Fatalf("empty scrape produced traffic: deltas=%#v online=%#v", deltas, online)
	}
	if got, ok := next["alice"]; !ok || got.up != 100 || got.down != 200 {
		t.Fatalf("missing user baseline = %#v, present=%v; want 100/200", got, ok)
	}

	next, deltas, _ = mergeCounterSnapshots(next, map[string]statsUser{
		"alice": {BytesIn: 175, BytesOut: 260},
	})
	if got := deltas["alice"]; got.up != 75 || got.down != 60 {
		t.Fatalf("reappearing user delta = %#v, want 75/60", got)
	}
	if got := next["alice"]; got.up != 175 || got.down != 260 {
		t.Fatalf("next baseline = %#v, want 175/260", got)
	}
}

func TestMergeCounterSnapshotsResetIsPerUser(t *testing.T) {
	previous := map[string]clientCounters{
		"alice": {up: 500, down: 600},
		"bob":   {up: 100, down: 200},
	}
	_, deltas, online := mergeCounterSnapshots(previous, map[string]statsUser{
		"alice": {BytesIn: 10, BytesOut: 20, Connections: 1},
		"bob":   {BytesIn: 130, BytesOut: 250},
	})

	if got := deltas["alice"]; got.up != 10 || got.down != 20 {
		t.Fatalf("reset delta = %#v, want 10/20", got)
	}
	if got := deltas["bob"]; got.up != 30 || got.down != 50 {
		t.Fatalf("normal delta = %#v, want 30/50", got)
	}
	if len(online) != 1 || online[0] != "alice" {
		t.Fatalf("online = %#v, want alice", online)
	}
}

func TestMergeCounterSnapshotsFirstObservationOnlySeedsBaseline(t *testing.T) {
	next, deltas, _ := mergeCounterSnapshots(nil, map[string]statsUser{
		"alice": {BytesIn: 100, BytesOut: 200},
	})
	if len(deltas) != 0 {
		t.Fatalf("first observation produced deltas: %#v", deltas)
	}
	if got := next["alice"]; got.up != 100 || got.down != 200 {
		t.Fatalf("first baseline = %#v, want 100/200", got)
	}
}
