package outbound

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAutomaticSpeedProbeThroughTunnel(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status, size int
		success      bool
	}{
		{"bounded download", 200, 2 << 20, true}, {"too small", 200, 1024, false}, {"error page", 403, 1 << 20, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(strings.Repeat("x", tc.size)))
			}))
			defer server.Close()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			go serveStubSocks(listener)
			_, rawPort, _ := net.SplitHostPort(listener.Addr().String())
			port, _ := strconv.Atoi(rawPort)
			result := &TestOutboundResult{Mode: "speed"}
			probeThroughSocks(port, server.URL, time.Second, true, result)
			if result.Success != tc.success {
				t.Fatalf("%+v", result)
			}
			if tc.success && (result.DownloadBytes != 1<<20 || result.DownloadMbps <= 0 || result.HTTPStatus != 200) {
				t.Fatalf("%+v", result)
			}
		})
	}
}
func TestAutomaticContextIncludesChainsExcludesUnrelated(t *testing.T) {
	items := `[{"tag":"proxy","protocol":"socks","streamSettings":{"sockopt":{"dialerProxy":"chain"}}}]`
	all := `[{"tag":"unrelated","protocol":"loopback"},{"tag":"chain","protocol":"socks","streamSettings":{"sockopt":{"dialerProxy":"proxy"}}},{"tag":"proxy","protocol":"blackhole"}]`
	result, err := automaticProbeContext(items, all)
	if err != nil {
		t.Fatal(err)
	}
	var obs []map[string]any
	if err := json.Unmarshal([]byte(result), &obs); err != nil {
		t.Fatal(err)
	}
	if len(obs) != 2 || obs[0]["protocol"] != "socks" || obs[1]["tag"] != "chain" {
		t.Fatal(result)
	}
}
func TestSingBoxProbeConfiguration(t *testing.T) {
	ob := map[string]any{"tag": "candidate", "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": "1.1.1.1", "port": 1080}}}}
	items := []*httpBatchItem{{tag: "candidate", outbound: ob, result: &TestOutboundResult{}}}
	source := buildBatchTestConfig(items, []any{ob}, []int{12345})
	cfg, err := buildSingBoxBatchTestConfig(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 1 || cfg.Inbounds[0]["listen"] != "127.0.0.1" || len(cfg.Outbounds) != 1 || cfg.Outbounds[0]["type"] != "socks" {
		t.Fatalf("%+v", cfg)
	}
	if _, err := cfg.Marshal(); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticContextIncludesNativeGroupAndDetour(t *testing.T) {
	items := `[{"tag":"group","protocol":"selector","settings":{"outbounds":["proxy"]}}]`
	all := `[{"tag":"proxy","protocol":"socks","settings":{"detour":"hop"}},{"tag":"hop","protocol":"socks"},{"tag":"unrelated","protocol":"loopback"}]`
	result, err := automaticProbeContext(items, all)
	if err != nil {
		t.Fatal(err)
	}
	var obs []map[string]any
	if err := json.Unmarshal([]byte(result), &obs); err != nil {
		t.Fatal(err)
	}
	if len(obs) != 3 || obs[0]["tag"] != "group" || obs[1]["tag"] != "proxy" || obs[2]["tag"] != "hop" {
		t.Fatal(result)
	}
}
