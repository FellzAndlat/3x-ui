package tuic

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsureFrontsSidecarWithRelayAndRemoveReleasesPort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the sidecar binary")
	}
	bin := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", bin)
	if err := os.WriteFile(filepath.Join(bin, GetBinaryName()), []byte("#!/bin/sh\nexec sleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	port, err := freeLoopbackUDPPort()
	if err != nil {
		t.Fatal(err)
	}
	inst := Instance{
		Id: 7, Tag: "tuic-7", Listen: "127.0.0.1", Port: port,
		Clients: []TuicClientSettings{{UUID: "u", Password: "p", Email: "e"}},
	}
	m := &Manager{procs: map[int]*managed{}, lastStartErr: map[int]string{}}
	t.Cleanup(m.StopAll)

	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}); err == nil {
		_ = c.Close()
		t.Fatal("the relay must own the inbound's public port while the sidecar runs")
	}
	raw, err := os.ReadFile(ConfigPathForID(7))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Server string `json:"server"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	host, sidecarPort, err := net.SplitHostPort(cfg.Server)
	if err != nil || host != "127.0.0.1" || sidecarPort == "" || cfg.Server == inst.BindTo() {
		t.Fatalf("sidecar bound to %q, want a loopback port other than the public %q", cfg.Server, inst.BindTo())
	}

	m.Remove(7)
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatalf("public port still held after Remove: %v", err)
	}
	_ = c.Close()
}

func TestEnsureUpdatesTagWithoutRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the sidecar binary")
	}
	bin := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", bin)
	if err := os.WriteFile(filepath.Join(bin, GetBinaryName()), []byte("#!/bin/sh\nexec sleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	port, err := freeLoopbackUDPPort()
	if err != nil {
		t.Fatal(err)
	}
	inst := Instance{
		Id: 8, Tag: "old-tag", Listen: "127.0.0.1", Port: port,
		Clients: []TuicClientSettings{{UUID: "u", Password: "p", Email: "e"}},
	}
	m := &Manager{procs: map[int]*managed{}, lastStartErr: map[int]string{}}
	t.Cleanup(m.StopAll)

	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	inst.Tag = "new-tag"
	if err := m.Ensure(inst); err != nil {
		t.Fatalf("Ensure updated tag: %v", err)
	}
	m.mu.Lock()
	gotTag := m.procs[8].tag
	m.mu.Unlock()
	if gotTag != "new-tag" {
		t.Fatalf("manager tag = %q, want %q", gotTag, "new-tag")
	}
}

func TestStopManagedCapturesFinalTraffic(t *testing.T) {
	relay, err := startUDPRelay("127.0.0.1:0", doublingEcho(t), relayFlowIdle)
	if err != nil {
		t.Fatal(err)
	}

	if got := roundTrip(t, relay, []byte("hello")); got != 10 {
		t.Fatalf("reply = %d bytes, want 10", got)
	}
	if !relay.bindPeer(onlyRelayPeer(t, relay), "alice@example.com") {
		t.Fatal("failed to bind relay peer")
	}

	m := &Manager{}
	m.stopManagedAndCaptureLocked(&managed{relay: relay, tag: "old-tag"}, "new-tag")
	snapshot := m.CollectTrafficSnapshot()
	if len(snapshot.Inbounds) != 1 {
		t.Fatalf("final inbound deltas = %#v, want one", snapshot.Inbounds)
	}
	if got := snapshot.Inbounds[0]; got.Tag != "new-tag" || got.Up != 5 || got.Down != 10 {
		t.Fatalf("final inbound delta = %#v, want new-tag 5 up / 10 down", got)
	}
	if len(snapshot.Clients) != 1 {
		t.Fatalf("final client deltas = %#v, want one", snapshot.Clients)
	}
	if got := snapshot.Clients[0]; got.Tag != "new-tag" || got.Email != "alice@example.com" || got.Up != 5 || got.Down != 10 {
		t.Fatalf("final client delta = %#v, want alice/new-tag 5 up / 10 down", got)
	}
	if again := m.CollectTrafficSnapshot(); len(again.Inbounds) != 0 || len(again.Clients) != 0 {
		t.Fatalf("final snapshot replayed twice: %#v", again)
	}
}
