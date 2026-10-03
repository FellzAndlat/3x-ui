package singbox

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSelectorLiveSwitch_E2E(t *testing.T) {
	binary := os.Getenv("SINGBOX_E2E_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_E2E_BINARY for real-core switching")
	}
	port := func() int {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		return listener.Addr().(*net.TCPAddr).Port
	}
	apiPort, socksPort := port(), port()
	cfg := map[string]any{
		"log":      map[string]any{"level": "error"},
		"inbounds": []any{map[string]any{"type": "socks", "tag": "test-in", "listen": "127.0.0.1", "listen_port": socksPort}},
		"outbounds": []any{
			map[string]any{"type": "selector", "tag": "sub-auto-7", "outbounds": []string{"sub-auto-block-7", "a", "b"}, "default": "a", "interrupt_exist_connections": false},
			map[string]any{"type": "direct", "tag": "a"}, map[string]any{"type": "direct", "tag": "b"}, map[string]any{"type": "block", "tag": "sub-auto-block-7"},
		},
		"route":        map[string]any{"final": "sub-auto-7"},
		"experimental": map[string]any{"clash_api": map[string]any{"external_controller": fmt.Sprintf("127.0.0.1:%d", apiPort), "secret": "test-secret"}},
	}
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
	client, err := NewSelectorClient(fmt.Sprintf("127.0.0.1:%d", apiPort), "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err = client.State(context.Background(), "sub-auto-7"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("selector never started", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
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
	if err = exchange(existing); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"b", "sub-auto-block-7", "a"} {
		if actual, err := client.Select(context.Background(), "sub-auto-7", target); err != nil || actual != target {
			t.Fatal(actual, err)
		}
		if err = exchange(existing); err != nil {
			t.Fatal("existing connection interrupted", err)
		}
		conn, err := dialer.Dial("tcp", listener.Addr().String())
		if target == "sub-auto-block-7" {
			if err == nil {
				_ = conn.SetDeadline(time.Now().Add(time.Second))
				if err = exchange(conn); err == nil {
					t.Fatal("block leaked traffic")
				}
				conn.Close()
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			if err = exchange(conn); err != nil {
				t.Fatal(err)
			}
			conn.Close()
		}
	}
}
