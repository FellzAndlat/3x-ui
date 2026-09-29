package mieru

import (
	"testing"
	"time"
)

func TestCollectInstanceTrafficPreservesCursorAcrossTemporaryAbsence(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	inst := Instance{
		Tag:   "mieru-test",
		Users: []User{{Name: "alice", Password: "secret"}},
	}
	cursors := make(map[string]trafficCursor)

	deltas, _ := collectInstanceTraffic(inst, cursors, map[string]userTrafficStats{
		"alice": {up: 100, down: 200, lastActive: now},
	}, now)
	if len(deltas) != 0 {
		t.Fatalf("first sample produced deltas: %#v", deltas)
	}

	deltas, _ = collectInstanceTraffic(inst, cursors, map[string]userTrafficStats{}, now.Add(time.Second))
	if len(deltas) != 0 {
		t.Fatalf("missing sample produced deltas: %#v", deltas)
	}
	if _, ok := cursors["alice"]; !ok {
		t.Fatal("configured user's cursor was removed while absent from scrape")
	}

	deltas, _ = collectInstanceTraffic(inst, cursors, map[string]userTrafficStats{
		"alice": {up: 175, down: 260, lastActive: now.Add(2 * time.Second)},
	}, now.Add(2*time.Second))
	if len(deltas) != 1 {
		t.Fatalf("deltas = %#v, want one delta", deltas)
	}
	if deltas[0].Up != 75 || deltas[0].Down != 60 {
		t.Fatalf("delta = up:%d down:%d, want up:75 down:60", deltas[0].Up, deltas[0].Down)
	}
}

func TestCollectInstanceTrafficPrunesRemovedUsers(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cursors := map[string]trafficCursor{
		"alice": {up: 100, down: 200, initialized: true},
		"bob":   {up: 300, down: 400, initialized: true},
	}
	inst := Instance{
		Tag:   "mieru-test",
		Users: []User{{Name: "alice", Password: "secret"}},
	}

	collectInstanceTraffic(inst, cursors, nil, now)
	if _, ok := cursors["alice"]; !ok {
		t.Fatal("configured user's cursor was pruned")
	}
	if _, ok := cursors["bob"]; ok {
		t.Fatal("removed user's cursor was retained")
	}
}

func TestCollectInstanceTrafficCounterResetIsIsolated(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	inst := Instance{
		Tag: "mieru-test",
		Users: []User{
			{Name: "alice", Password: "a"},
			{Name: "bob", Password: "b"},
		},
	}
	cursors := map[string]trafficCursor{
		"alice": {up: 500, down: 600, initialized: true},
		"bob":   {up: 100, down: 200, initialized: true},
	}

	deltas, _ := collectInstanceTraffic(inst, cursors, map[string]userTrafficStats{
		"alice": {up: 10, down: 20, lastActive: now},
		"bob":   {up: 130, down: 250, lastActive: now},
	}, now)
	if len(deltas) != 1 {
		t.Fatalf("deltas = %#v, want only bob increment", deltas)
	}
	if deltas[0].Email != "bob" || deltas[0].Up != 30 || deltas[0].Down != 50 {
		t.Fatalf("unexpected delta after reset: %#v", deltas[0])
	}
	if cur := cursors["alice"]; cur.up != 10 || cur.down != 20 {
		t.Fatalf("alice reset cursor = %#v, want 10/20", cur)
	}
}

func TestCollectInstanceTrafficIgnoresStaleUnconfiguredUsers(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	inst := Instance{
		Tag:   "mieru-test",
		Users: []User{{Name: "alice", Password: "secret"}},
	}
	cursors := map[string]trafficCursor{
		"alice": {up: 100, down: 200, initialized: true},
	}

	deltas, online := collectInstanceTraffic(inst, cursors, map[string]userTrafficStats{
		"alice": {up: 125, down: 240, lastActive: now},
		"ghost": {up: 999, down: 999, lastActive: now},
	}, now)
	if len(deltas) != 1 || deltas[0].Email != "alice" {
		t.Fatalf("unexpected deltas: %#v", deltas)
	}
	if len(online) != 1 || online[0] != "alice" {
		t.Fatalf("unexpected online users: %#v", online)
	}
}
