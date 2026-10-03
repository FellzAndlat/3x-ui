package xray

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// Real traffic proves the permanent loopback switches new connections while
// an established connection survives a routing-only API change.
func TestAutomaticLoopbackLiveSwitch_E2E(t *testing.T) {
	binary := os.Getenv("XRAY_E2E_BINARY")
	if binary == "" {
		t.Skip("set XRAY_E2E_BINARY to run real-core switching")
	}
	apiPort, socksPort := freePort(t), freePort(t)
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"api": map[string]any{"tag": "api", "services": []string{"HandlerService", "RoutingService"}},
		"inbounds": []any{
			map[string]any{"tag": "api", "listen": "127.0.0.1", "port": apiPort, "protocol": "tunnel", "settings": map[string]any{"rewriteAddress": "127.0.0.1"}},
			map[string]any{"tag": "test-in", "listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth"}},
		},
		"outbounds": []any{
			map[string]any{"tag": "sub-auto-7", "protocol": "loopback", "settings": map[string]any{"inboundTag": "sub-auto-route-7"}},
			map[string]any{"tag": "a", "protocol": "freedom", "settings": map[string]any{}},
			map[string]any{"tag": "sub-auto-block-7", "protocol": "blackhole", "settings": map[string]any{}},
		},
	}
	routing := func(target string) []byte {
		data, _ := json.Marshal(map[string]any{"domainStrategy": "AsIs", "rules": []any{
			map[string]any{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"},
			map[string]any{"type": "field", "inboundTag": []string{"sub-auto-route-7"}, "outboundTag": target},
			map[string]any{"type": "field", "inboundTag": []string{"test-in"}, "outboundTag": "sub-auto-7"},
		}})
		return data
	}
	cfg["routing"] = json.RawMessage(routing("a"))
	data, _ := json.Marshal(cfg)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-c", path)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	waitForPort(t, apiPort)
	waitForPort(t, socksPort)
	api := &XrayAPI{}
	if err := api.Init(apiPort); err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); _, _ = io.Copy(conn, conn) }()
		}
	}()
	dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), nil, &net.Dialer{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	connect := func() net.Conn {
		t.Helper()
		conn, err := dialer.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
	exchange := func(conn net.Conn) error {
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.Write([]byte("ok")); err != nil {
			return err
		}
		buf := make([]byte, 2)
		_, err := io.ReadFull(conn, buf)
		if err == nil && string(buf) != "ok" {
			return fmt.Errorf("wrong echo")
		}
		return err
	}
	existing := connect()
	defer existing.Close()
	if err := exchange(existing); err != nil {
		t.Fatal(err)
	}
	if err := api.ApplyRoutingConfig(routing("sub-auto-block-7")); err != nil {
		t.Fatal(err)
	}
	actual, err := api.TestRoute(RouteTestRequest{InboundTag: "sub-auto-route-7", IP: "1.1.1.1", Port: 443})
	if err != nil || actual.OutboundTag != "sub-auto-block-7" {
		t.Fatal(actual, err)
	}
	if err := exchange(existing); err != nil {
		t.Fatal("established connection interrupted", err)
	}
	blocked, err := dialer.Dial("tcp", listener.Addr().String())
	if err == nil {
		defer blocked.Close()
		if err = exchange(blocked); err == nil {
			t.Fatal("failed route leaked traffic")
		}
	}
	if err := api.ApplyRoutingConfig(routing("a")); err != nil {
		t.Fatal(err)
	}
	recovered := connect()
	defer recovered.Close()
	if err := exchange(recovered); err != nil {
		t.Fatal("route did not recover", err)
	}
	if err := exchange(existing); err != nil {
		t.Fatal("existing connection lost during recovery", err)
	}
}
