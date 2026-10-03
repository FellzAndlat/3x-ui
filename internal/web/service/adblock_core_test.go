package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
	"golang.org/x/net/proxy"
)

// Opt-in checks exercise real core configuration loaders and IP-addressed HTTP
// traffic. CI can provide binaries without making regular unit tests download
// or depend on external executables.
func TestAdBlockRealCores(t *testing.T) {
	for _, core := range []struct{ name, env string }{
		{"xray", "XUI_ADBLOCK_XRAY_BINARY"}, {"singbox", "XUI_ADBLOCK_SINGBOX_BINARY"},
	} {
		t.Run(core.name, func(t *testing.T) {
			binary := os.Getenv(core.env)
			if binary == "" {
				t.Skip("set " + core.env + " to run the real core check")
			}
			setupBulkDB(t)
			settings := &SettingService{}
			youtubeMode := "privacy"
			if err := settings.SaveAdBlockSettings(context.Background(), AdBlockSettings{
				Enabled: true, YoutubeMode: &youtubeMode, CustomDomains: "ads.example\ndomain:tracker.example\ndomain:broad.example\nexact.broad.example\ndomain:googlevideo.com\ndomain:youtube.com\nyoutubei.googleapis.com\n", Allowlist: "safe.broad.example",
			}); err != nil {
				t.Fatal(err)
			}
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("allowed")) }))
			defer origin.Close()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()
			configPath := filepath.Join(t.TempDir(), "config.json")
			var config []byte
			if core.name == "xray" {
				cfg := &xray.Config{
					LogConfig:       json_util.RawMessage(`{"loglevel":"none"}`),
					RouterConfig:    json_util.RawMessage(`{"domainStrategy":"AsIs","rules":[]}`),
					OutboundConfigs: json_util.RawMessage(`[{"tag":"direct","protocol":"freedom"}]`),
					InboundConfigs:  []xray.InboundConfig{{Tag: "test", Protocol: "socks", Listen: json_util.RawMessage(`"127.0.0.1"`), Port: port, Settings: json_util.RawMessage(`{"auth":"noauth"}`)}},
				}
				if err := settings.applyXrayAdBlock(cfg); err != nil {
					t.Fatal(err)
				}
				config, err = json.Marshal(cfg)
			} else {
				cfg := &singbox.Config{
					Log:       map[string]any{"level": "error"},
					Inbounds:  []map[string]any{{"type": "socks", "tag": "test", "listen": "127.0.0.1", "listen_port": port}},
					Outbounds: []map[string]any{{"type": "direct", "tag": "direct"}},
					Route:     map[string]any{"final": "direct"},
				}
				if err := settings.applySingBoxAdBlock(cfg); err != nil {
					t.Fatal(err)
				}
				config, err = cfg.Marshal()
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, config, 0600); err != nil {
				t.Fatal(err)
			}
			validateArgs := []string{"check", "-c", configPath}
			runArgs := []string{"run", "-c", configPath}
			if core.name == "xray" {
				validateArgs = []string{"run", "-test", "-config", configPath}
				runArgs = []string{"run", "-config", configPath}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if output, err := exec.CommandContext(ctx, binary, validateArgs...).CombinedOutput(); err != nil {
				t.Fatalf("core rejects config: %v: %s", err, output)
			}
			logPath := filepath.Join(filepath.Dir(configPath), "core.log")
			logFile, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			defer logFile.Close()
			command := exec.CommandContext(ctx, binary, runArgs...)
			command.Stdout, command.Stderr = logFile, logFile
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
			address := fmt.Sprintf("127.0.0.1:%d", port)
			ready := false
			for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
				conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
				if err == nil {
					_ = conn.Close()
					ready = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !ready {
				log, _ := os.ReadFile(logPath)
				if strings.Contains(string(log), "create netlink socket: operation not permitted") {
					t.Skip("core configuration accepted; runtime requires netlink sockets unavailable in this environment")
				}
				t.Fatalf("core did not start: %s", log)
			}
			dialer, err := proxy.SOCKS5("tcp", address, nil, &net.Dialer{Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
				return dialer.Dial(network, address)
			}, DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
			for _, test := range []struct {
				host    string
				blocked bool
			}{
				{"ads.example", true}, {"sub.ads.example", false}, {"sub.tracker.example", true},
				{"safe.broad.example", false}, {"exact.broad.example", true}, {"ordinary.example", false},
				{"r1.googlevideo.com", false}, {"www.youtube.com", false}, {"youtubei.googleapis.com", false}, {"googleads.g.doubleclick.net", true},
			} {
				req, _ := http.NewRequest(http.MethodGet, origin.URL, nil)
				req.Host = test.host
				response, err := client.Do(req)
				if response != nil {
					_ = response.Body.Close()
				}
				if test.blocked && err == nil {
					t.Errorf("%s passed the filter", test.host)
				}
				if !test.blocked && err != nil {
					t.Errorf("%s was unexpectedly blocked: %v", test.host, err)
				}
			}
			// Domain-only ABP rules are stored as suffix rules, not HTTP path filters.
			raw, _ := settings.GetAdBlockDomains()
			if !strings.Contains(raw, "domain:tracker.example") {
				t.Fatal("suffix type lost during persistence")
			}
		})
	}
}
