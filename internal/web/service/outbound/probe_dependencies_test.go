package outbound

import (
	"errors"
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func TestSelectProbeOutboundsKeepsOnlyDependencyClosure(t *testing.T) {
	outbounds := []map[string]any{
		{"tag": "unused", "protocol": "broken"},
		{"tag": "hop", "protocol": "ssh", "settings": map[string]any{"server": "example.com"}},
		{"tag": "proxy", "protocol": "singbox:shadowsocks", "settings": map[string]any{"detour": "hop"}},
		{"tag": "group", "protocol": "singbox:selector", "settings": map[string]any{"outbounds": []any{"proxy"}}},
	}
	selected, err := selectProbeOutbounds(outbounds, []string{"group"})
	if err != nil || len(selected) != 3 {
		t.Fatalf("selected=%v err=%v", selected, err)
	}
	for i, tag := range []string{"hop", "proxy", "group"} {
		if selected[i]["tag"] != tag {
			t.Fatalf("selected=%v", selected)
		}
	}
}

func TestSelectProbeOutboundsKeepsProxySettingsDependency(t *testing.T) {
	outbounds := []map[string]any{
		{"tag": "hop", "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": 1080}}}},
		{"tag": "proxy", "protocol": "vless", "proxySettings": map[string]any{"tag": "hop"}},
	}
	selected, err := selectProbeOutbounds(outbounds, []string{"proxy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0]["tag"] != "hop" || selected[1]["tag"] != "proxy" {
		t.Fatalf("proxySettings dependency was dropped: %v", selected)
	}
}

func TestSelectProbeOutboundsValidatesChains(t *testing.T) {
	tests := []struct {
		name      string
		outbounds []map[string]any
		want      string
	}{
		{"missing", []map[string]any{{"tag": "proxy", "settings": map[string]any{"detour": "missing"}}}, `dependency "missing" does not exist`},
		{"cycle", []map[string]any{{"tag": "a", "settings": map[string]any{"detour": "b"}}, {"tag": "b", "settings": map[string]any{"detour": "a"}}}, "a -> b -> a"},
		{"duplicate", []map[string]any{{"tag": "proxy"}, {"tag": "proxy"}}, `duplicate outbound tag "proxy"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, _ := test.outbounds[0]["tag"].(string)
			_, err := selectProbeOutbounds(test.outbounds, []string{root})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v want=%s", err, test.want)
			}
		})
	}
}

func TestProbeDependenciesPreferSockoptDetour(t *testing.T) {
	raw := map[string]any{"tag": "proxy", "protocol": "singbox:ssh", "settings": map[string]any{"detour": "obsolete"}, "streamSettings": map[string]any{"sockopt": map[string]any{"dialerProxy": " hop "}}}
	selected, err := selectProbeOutbounds([]map[string]any{raw, {"tag": "hop"}}, []string{"proxy"})
	if err != nil || len(selected) != 2 {
		t.Fatalf("selected=%v err=%v", selected, err)
	}
}

func TestSingBoxRetryExcludesUnrelatedInvalidOutbound(t *testing.T) {
	t.Setenv("XUI_BIN_FOLDER", t.TempDir())
	old := newSingBoxBatchProcess
	defer func() { newSingBoxBatchProcess = old }()
	calls := 0
	newSingBoxBatchProcess = func(cfg *singbox.Config, _ string) batchProcess {
		calls++
		for _, outbound := range cfg.Outbounds {
			if outbound["type"] != "ssh" {
				t.Fatalf("unrelated bad outbound reached process: %v", cfg.Outbounds)
			}
		}
		return &stubProcess{cfg: &xray.Config{}, startErr: errors.New("isolated-good")}
	}
	valid := map[string]any{"tag": "good", "protocol": "ssh", "settings": map[string]any{"server": "example.com"}}
	broken := map[string]any{"tag": "bad", "protocol": "singbox:tuic", "settings": map[string]any{}}
	results, err := (&OutboundService{CoreType: "sing-box"}).TestOutbounds(mustJSON(t, []any{valid, broken}), "http://example.invalid", mustJSON(t, []any{valid, broken}), "http")
	if err != nil || len(results) != 2 || !strings.Contains(results[1].Error, "requires a server") {
		t.Fatalf("results=%+v err=%v", results, err)
	}

	if results[0].Error != "Failed to start test sing-box instance: isolated-good" {
		t.Fatalf("valid outbound inherited sibling error: %s", results[0].Error)
	}
	if calls != 1 {
		t.Fatalf("valid item never reached isolated process: %d", calls)
	}
}

func TestChainedOutboundsRequireHandshakeProbe(t *testing.T) {
	for _, raw := range []map[string]any{
		{"protocol": "singbox:shadowsocks", "settings": map[string]any{"detour": "shadowtls"}},
		{"protocol": "vless", "streamSettings": map[string]any{"sockopt": map[string]any{"dialerProxy": "hop"}}},
		{"protocol": "singbox:vmess", "settings": map[string]any{"transport": map[string]any{"type": "quic"}}},
	} {
		if !outboundTransportIsUDP(raw) {
			t.Fatalf("chain uses bare TCP probe: %v", raw)
		}
	}
}
