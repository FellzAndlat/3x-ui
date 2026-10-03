package service

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func scopedXrayConfig() *xray.Config {
	return &xray.Config{
		LogConfig:       json_util.RawMessage(`{"loglevel":"none"}`),
		RouterConfig:    json_util.RawMessage(`{"rules":[{"type":"field","network":"tcp","outboundTag":"direct"}]}`),
		OutboundConfigs: json_util.RawMessage(`[{"tag":"direct","protocol":"freedom"}]`),
		InboundConfigs: []xray.InboundConfig{
			{Tag: "home", Protocol: "vless", Listen: json_util.RawMessage(`"127.0.0.1"`), Port: 21000, Settings: json_util.RawMessage(`{"decryption":"none","clients":[{"id":"11111111-1111-4111-8111-111111111111","email":"alice"},{"id":"22222222-2222-4222-8222-222222222222","email":"bob"}]}`)},
			{Tag: "office", Protocol: "socks", Listen: json_util.RawMessage(`"127.0.0.1"`), Port: 21001, Settings: json_util.RawMessage(`{"auth":"noauth"}`)},
		},
	}
}
func TestAdBlockXrayScopePreservesOtherRouting(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	scope := AdBlockScope{InboundMode: "include", Inbounds: []string{"home"}, ClientMode: "exclude", Clients: []string{"alice"}}
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "ads.example", Scope: &scope}); err != nil {
		t.Fatal(err)
	}
	cfg := scopedXrayConfig()
	for i := 0; i < 2; i++ {
		if err := s.applyXrayAdBlock(cfg); err != nil {
			t.Fatal(err)
		}
		var routing struct {
			Rules []struct {
				OutboundTag string   `json:"outboundTag"`
				InboundTag  []string `json:"inboundTag"`
				User        []string `json:"user"`
			}
		}
		if err := json.Unmarshal(cfg.RouterConfig, &routing); err != nil {
			t.Fatal(err)
		}
		if len(routing.Rules) != 2 || routing.Rules[0].OutboundTag != adblock.OutboundTag || len(routing.Rules[0].InboundTag) != 1 || routing.Rules[0].InboundTag[0] != "home" || len(routing.Rules[0].User) != 1 || routing.Rules[0].User[0] != "bob" || routing.Rules[1].OutboundTag != "direct" {
			t.Fatalf("scope overrides routing: %s", cfg.RouterConfig)
		}
	}
	// A newly added client joins an exclude scope when the core config regenerates.
	cfg = scopedXrayConfig()
	cfg.InboundConfigs[0].Settings = json_util.RawMessage(`{"decryption":"none","clients":[{"id":"11111111-1111-4111-8111-111111111111","email":"alice"},{"id":"22222222-2222-4222-8222-222222222222","email":"bob"},{"id":"33333333-3333-4333-8333-333333333333","email":"carol"}]}`)
	if err := s.applyXrayAdBlock(cfg); err != nil {
		t.Fatal(err)
	}
	var route map[string]any
	_ = json.Unmarshal(cfg.RouterConfig, &route)
	users := route["rules"].([]any)[0].(map[string]any)["user"].([]any)
	if len(users) != 2 {
		t.Fatal("new client escaped exclude-mode filtering")
	}
	scope.InboundMode = "all"
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "ads.example", Scope: &scope}); err != nil {
		t.Fatal(err)
	}
	cfg = scopedXrayConfig()
	duplicate := cfg.InboundConfigs[0]
	duplicate.Tag = "home-two"
	duplicate.Port = 21002
	cfg.InboundConfigs = append(cfg.InboundConfigs, duplicate)
	if err := s.applyXrayAdBlock(cfg); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(cfg.RouterConfig, &route)
	scopedRules := route["rules"].([]any)
	if len(scopedRules) != 3 {
		t.Fatalf("large domain list duplicated for each inbound: %s", cfg.RouterConfig)
	}
	anonymous := scopedRules[0].(map[string]any)
	named := scopedRules[1].(map[string]any)
	if anonymous["inboundTag"].([]any)[0] != "office" || len(named["inboundTag"].([]any)) != 2 || named["user"].([]any)[0] != "bob" {
		t.Fatalf("bad mixed anonymous/authenticated scope: %s", cfg.RouterConfig)
	}
	scope.InboundMode = "include"
	scope.Inbounds = []string{}
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "ads.example", Scope: &scope}); err != nil {
		t.Fatal(err)
	}
	cfg = scopedXrayConfig()
	if err := s.applyXrayAdBlock(cfg); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(cfg.RouterConfig, &route)
	if len(route["rules"].([]any)) != 1 {
		t.Fatal("empty include scope blocked all traffic")
	}
}
func TestAdBlockSingBoxLogicalScopeRoundTrip(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	scope := AdBlockScope{InboundMode: "exclude", Inbounds: []string{"office"}, ClientMode: "exclude", Clients: []string{"alice"}}
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "ads.example", Scope: &scope}); err != nil {
		t.Fatal(err)
	}
	cfg := singbox.NewConfig()
	cfg.Route["rules"] = []map[string]any{{"action": "route", "outbound": "direct"}}
	for i := 0; i < 2; i++ {
		if err := s.applySingBoxAdBlock(cfg); err != nil {
			t.Fatal(err)
		}
		rules := cfg.Route["rules"].([]map[string]any)
		if len(rules) != 3 || rules[1]["type"] != "logical" || rules[1]["mode"] != "and" || rules[2]["outbound"] != "direct" {
			t.Fatalf("bad scoped rules: %+v", rules)
		}
		conditions := rules[1]["rules"].([]map[string]any)
		if len(conditions) != 3 || conditions[1]["invert"] != true || conditions[2]["invert"] != true {
			t.Fatal("scope exclusion not enforced")
		}
		raw, err := cfg.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, cfg); err != nil {
			t.Fatal(err)
		}
	}
	if err := stripSingBoxAdBlock(cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Route["rules"].([]map[string]any)) != 1 {
		t.Fatal("native editor retains managed logical scope")
	}
}
func TestAdBlockScopedConfigsAcceptedByRealCores(t *testing.T) {
	for _, core := range []struct{ Name, Env string }{{"xray", "XUI_ADBLOCK_XRAY_BINARY"}, {"singbox", "XUI_ADBLOCK_SINGBOX_BINARY"}} {
		t.Run(core.Name, func(t *testing.T) {
			binary := os.Getenv(core.Env)
			if binary == "" {
				t.Skip("set " + core.Env)
			}
			setupBulkDB(t)
			s := &SettingService{}
			scope := AdBlockScope{InboundMode: "include", Inbounds: []string{"home"}, ClientMode: "exclude", Clients: []string{"alice"}}
			if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "ads.example", Scope: &scope}); err != nil {
				t.Fatal(err)
			}
			var raw []byte
			var err error
			args := []string{"check", "-c"}
			if core.Name == "xray" {
				cfg := scopedXrayConfig()
				if err := s.applyXrayAdBlock(cfg); err != nil {
					t.Fatal(err)
				}
				raw, err = json.Marshal(cfg)
				args = []string{"run", "-test", "-config"}
			} else {
				cfg := &singbox.Config{Log: map[string]any{"level": "error"}, Inbounds: []map[string]any{{"type": "vless", "tag": "home", "listen": "127.0.0.1", "listen_port": 21000, "users": []map[string]any{{"name": "alice", "uuid": "11111111-1111-4111-8111-111111111111"}, {"name": "bob", "uuid": "22222222-2222-4222-8222-222222222222"}}}}, Outbounds: []map[string]any{{"type": "direct", "tag": "direct"}}, Route: map[string]any{"final": "direct"}}
				if err := s.applySingBoxAdBlock(cfg); err != nil {
					t.Fatal(err)
				}
				raw, err = cfg.Marshal()
			}
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if output, err := exec.CommandContext(ctx, binary, append(args, path)...).CombinedOutput(); err != nil {
				t.Fatalf("core rejects scoped config: %v %s", err, output)
			}
		})
	}
}
