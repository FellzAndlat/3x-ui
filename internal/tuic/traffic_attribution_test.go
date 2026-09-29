package tuic

import (
	"bytes"
	"testing"
	"time"
)

func onlyRelayPeer(t *testing.T, relay *udpRelay) string {
	t.Helper()
	relay.mu.Lock()
	defer relay.mu.Unlock()
	if len(relay.peers) != 1 {
		t.Fatalf("relay peer count = %d, want 1", len(relay.peers))
	}
	for peer := range relay.peers {
		return peer
	}
	return ""
}

func TestUDPRelayAttributesBytesReceivedBeforeAuthentication(t *testing.T) {
	relay, err := startUDPRelay("127.0.0.1:0", doublingEcho(t), relayFlowIdle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.Close)

	if got := roundTrip(t, relay, bytes.Repeat([]byte("a"), 100)); got != 200 {
		t.Fatalf("reply = %d bytes, want 200", got)
	}

	peer := onlyRelayPeer(t, relay)
	if deltas := relay.CollectClientTraffic(); len(deltas) != 0 {
		t.Fatalf("unauthenticated flow leaked client traffic: %#v", deltas)
	}
	if !relay.bindPeer(peer, "alice@example.com") {
		t.Fatalf("failed to bind relay peer %q", peer)
	}

	deltas := relay.CollectClientTraffic()
	if len(deltas) != 1 {
		t.Fatalf("client delta count = %d, want 1: %#v", len(deltas), deltas)
	}
	if got := deltas[0]; got.Email != "alice@example.com" || got.Up != 100 || got.Down != 200 {
		t.Fatalf("client delta = %#v, want alice 100 up / 200 down", got)
	}
	if deltas := relay.CollectClientTraffic(); len(deltas) != 0 {
		t.Fatalf("second client collect must be empty, got %#v", deltas)
	}
}

func TestUDPRelayPreservesBoundTrafficWhenFlowExpires(t *testing.T) {
	relay, err := startUDPRelay("127.0.0.1:0", doublingEcho(t), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.Close)

	if got := roundTrip(t, relay, []byte("hello")); got != 10 {
		t.Fatalf("reply = %d bytes, want 10", got)
	}
	if !relay.bindPeer(onlyRelayPeer(t, relay), "alice@example.com") {
		t.Fatal("failed to bind relay peer")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		relay.mu.Lock()
		flows := len(relay.flows)
		relay.mu.Unlock()
		if flows == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bound relay flow did not expire")
		}
		time.Sleep(10 * time.Millisecond)
	}

	deltas := relay.CollectClientTraffic()
	if len(deltas) != 1 || deltas[0].Email != "alice@example.com" || deltas[0].Up != 5 || deltas[0].Down != 10 {
		t.Fatalf("retired client traffic = %#v, want alice 5 up / 10 down", deltas)
	}
}

func TestTuicLogPeerForUUID(t *testing.T) {
	uuid := "123e4567-e89b-12d3-a456-426614174000"
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "plain ipv4",
			line: "[0x0000002a] [127.0.0.1:41327] [" + uuid + "] [authenticate] " + uuid,
			want: "127.0.0.1:41327",
		},
		{
			name: "logger prefix",
			line: "[2026-09-29T12:00:00Z INFO tuic_server] [0x0000002a] [127.0.0.1:41327] [" + uuid + "] [heartbeat]",
			want: "127.0.0.1:41327",
		},
		{
			name: "ipv6",
			line: "[0x0000002a] [[::1]:41327] [" + uuid + "] [authenticate] " + uuid,
			want: "[::1]:41327",
		},
		{
			name: "uuid only in payload",
			line: "[0x0000002a] [127.0.0.1:41327] [unauthenticated] auth failed for " + uuid,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tuicLogPeerForUUID(tt.line, uuid); got != tt.want {
				t.Fatalf("tuicLogPeerForUUID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProcLogWriterBindsOnlyAuthenticatedPeer(t *testing.T) {
	uuid := "123e4567-e89b-12d3-a456-426614174000"
	var gotPeer, gotEmail string
	w := &procLogWriter{
		label:       "test",
		uuidToEmail: map[string]string{uuid: "alice@example.com"},
		lastActive:  make(map[string]int64),
		bindPeer: func(peer, email string) bool {
			gotPeer, gotEmail = peer, email
			return true
		},
	}

	w.emitLocked("[0x0000002a] [127.0.0.1:41327] [unauthenticated] authentication failed for " + uuid)
	if gotPeer != "" || gotEmail != "" {
		t.Fatalf("failed authentication created binding (%q, %q)", gotPeer, gotEmail)
	}
	if w.lastActive["alice@example.com"] != 0 {
		t.Fatal("failed authentication marked client active")
	}

	w.emitLocked("[0x0000002a] [127.0.0.1:41327] [" + uuid + "] [authenticate] " + uuid)
	if gotPeer != "127.0.0.1:41327" || gotEmail != "alice@example.com" {
		t.Fatalf("binding = (%q, %q), want peer and alice email", gotPeer, gotEmail)
	}
	if w.lastActive["alice@example.com"] == 0 {
		t.Fatal("authenticated log did not update lastActive")
	}
}
