package service

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func policyFixture() adBlockPlan {
	all := AdBlockScope{InboundMode: "all", ClientMode: "all"}
	off := all
	off.ClientMode = "include"
	off.Clients = []string{"alice"}
	light := all
	light.ClientMode = "include"
	light.Clients = []string{"alice", "bob"}
	return adBlockPlan{Enabled: true, Scope: all, Base: []string{"strict.example"}, Policies: []AdBlockPolicy{
		{ID: "off", Name: "Off", Enabled: true, Profile: "off", Scope: off},
		{ID: "light", Name: "Light", Enabled: true, Profile: "light", Scope: light},
	}, Domains: map[string][]string{"light": {"light.example"}}}
}
func TestAdBlockPolicyFirstMatchAndMasterScope(t *testing.T) {
	p := policyFixture()
	for _, tc := range []struct {
		client, id string
		enabled    bool
	}{{"alice", "off", false}, {"bob", "light", true}, {"carol", "", true}} {
		id, on := planPolicyID(p, "in", tc.client)
		if id != tc.id || on != tc.enabled {
			t.Fatalf("%s: %s %v", tc.client, id, on)
		}
	}
	p.Scope.InboundMode = "include"
	p.Scope.Inbounds = []string{"other"}
	if _, on := planPolicyID(p, "in", "bob"); on {
		t.Fatal("policy escaped master scope")
	}
	p = policyFixture()
	p.Policies[0].Enabled = false
	if id, on := planPolicyID(p, "in", "alice"); id != "light" || !on {
		t.Fatal("disabled policy still takes precedence")
	}
}
func TestAdBlockPolicyXrayDoesNotInheritOrCrossProduct(t *testing.T) {
	p := policyFixture()
	cfg := &xray.Config{RouterConfig: json_util.RawMessage(`{"rules":[{"type":"field","network":"tcp","outboundTag":"direct"}]}`), InboundConfigs: []xray.InboundConfig{
		{Tag: "one", Settings: json_util.RawMessage(`{"clients":[{"email":"alice"},{"email":"bob"},{"email":"carol"}]}`)},
		{Tag: "two", Settings: json_util.RawMessage(`{"clients":[{"email":"bob"},{"email":"dave"}]}`)},
	}}
	if err := applyXrayPolicyPlan(cfg, p); err != nil {
		t.Fatal(err)
	}
	var router struct {
		Rules []struct {
			User     []string `json:"user"`
			Domain   []string `json:"domain"`
			Inbound  []string `json:"inboundTag"`
			Outbound string   `json:"outboundTag"`
		}
	}
	if err := json.Unmarshal(cfg.RouterConfig, &router); err != nil {
		t.Fatal(err)
	}
	for _, r := range router.Rules {
		if r.Outbound != adblock.OutboundTag {
			continue
		}
		for _, u := range r.User {
			if u == "alice" {
				t.Fatal("Off inherited block rules")
			}
			if u == "bob" && strings.Join(r.Domain, ",") != "full:light.example" {
				t.Fatal("Light inherited strict domains")
			}
			if u == "carol" && strings.Join(r.Inbound, ",") != "one" {
				t.Fatal("client/inbound cross product")
			}
			if u == "dave" && strings.Join(r.Inbound, ",") != "two" {
				t.Fatal("client/inbound cross product")
			}
		}
	}
	if len(router.Rules) != 4 {
		t.Fatalf("unexpected groups: %s", cfg.RouterConfig)
	}
	if err := applyXrayPolicyPlan(cfg, p); err != nil {
		t.Fatal(err)
	}
	var again map[string]any
	_ = json.Unmarshal(cfg.RouterConfig, &again)
	if len(again["rules"].([]any)) != 4 {
		t.Fatal("repeat application duplicated rules")
	}
}
func TestAdBlockPolicySingBoxPriorityAndCleanup(t *testing.T) {
	cfg := singbox.NewConfig()
	cfg.Route["rules"] = []map[string]any{{"action": "route", "outbound": "direct"}}
	if err := applySingBoxPolicyPlan(cfg, policyFixture()); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(cfg.Route)
	text := string(raw)
	if !strings.Contains(text, `"invert":true`) || !strings.Contains(text, `"auth_user":["alice"]`) {
		t.Fatalf("Off exclusion missing: %s", text)
	}
	sets := cfg.Route["rule_set"].([]map[string]any)
	if len(sets) != 2 {
		t.Fatalf("off generated a set: %s", text)
	}
	if err := applySingBoxPolicyPlan(cfg, policyFixture()); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Route["rules"].([]map[string]any)) != 4 {
		t.Fatal("duplicate rules")
	}
	if err := applySingBoxPolicyPlan(cfg, adBlockPlan{}); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Route["rules"].([]map[string]any)) != 1 || len(cfg.Route["rule_set"].([]map[string]any)) != 0 {
		t.Fatal("policy rules survived disable")
	}
}
func TestAdBlockPolicyFingerprintAndValidation(t *testing.T) {
	values := map[string]string{}
	for k, v := range defaultValueMap {
		values[k] = v
	}
	values["adBlockEnable"] = "true"
	values["adBlockDomains"] = "strict.example"
	p := policyFixture()
	policies, _ := json.Marshal(p.Policies)
	values["adBlockPolicies"] = string(policies)
	values["adBlockPolicyDomains"] = `{"light":"light.example"}`
	now := time.Now()
	first, err := adBlockFingerprint(values, now)
	if err != nil {
		t.Fatal(err)
	}
	p.Policies[0].Name = "Renamed"
	policies, _ = json.Marshal(p.Policies)
	values["adBlockPolicies"] = string(policies)
	second, _ := adBlockFingerprint(values, now)
	if first != second {
		t.Fatal("rename schedules restart")
	}
	p.Policies[0], p.Policies[1] = p.Policies[1], p.Policies[0]
	policies, _ = json.Marshal(p.Policies)
	values["adBlockPolicies"] = string(policies)
	third, _ := adBlockFingerprint(values, now)
	if third == first {
		t.Fatal("priority change not applied")
	}
	values["adBlockPausedUntil"] = now.Add(time.Hour).Format(time.RFC3339)
	before, _ := adBlockFingerprint(values, now)
	after, _ := adBlockFingerprint(values, now.Add(2*time.Hour))
	if before == after {
		t.Fatal("pause expiry ignored")
	}
	p.Policies[1].ID = p.Policies[0].ID
	if validateAdBlockPolicies(p.Policies) == nil {
		t.Fatal("duplicate ID accepted")
	}
}

func TestAdBlockPolicyRealCores(t *testing.T) {
	for _, core := range []struct{ name, env string }{{"xray", "XUI_ADBLOCK_XRAY_BINARY"}, {"singbox", "XUI_ADBLOCK_SINGBOX_BINARY"}} {
		t.Run(core.name, func(t *testing.T) {
			binary := os.Getenv(core.env)
			if binary == "" {
				t.Skip("set " + core.env)
			}
			var raw []byte
			var err error
			args := []string{"check", "-c"}
			if core.name == "xray" {
				cfg := scopedXrayConfig()
				err = applyXrayPolicyPlan(cfg, policyFixture())
				if err == nil {
					raw, err = json.Marshal(cfg)
				}
				args = []string{"run", "-test", "-config"}
			} else {
				cfg := singbox.NewConfig()
				cfg.Outbounds = []map[string]any{{"type": "direct", "tag": "direct"}}
				cfg.Route["final"] = "direct"
				err = applySingBoxPolicyPlan(cfg, policyFixture())
				if err == nil {
					raw, err = cfg.Marshal()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if output, err := exec.CommandContext(ctx, binary, append(args, path)...).CombinedOutput(); err != nil {
				t.Fatalf("core rejected policies: %v %s", err, output)
			}
		})
	}
}
func TestAdBlockPolicyCachedSaveAndDiagnostic(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	profile, _ := findAdBlockProfile("light")
	url := splitAdBlockSources(profile.Sources)[0]
	cache, _ := json.Marshal(map[string]adblock.SourceCache{url: {Domains: []string{"light.example"}}})
	if err := saveAdBlockValues(map[string]string{"adBlockSourceCache": string(cache)}); err != nil {
		t.Fatal(err)
	}
	p := policyFixture()
	policies := p.Policies
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "custom.example", Policies: &policies}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		client, host, profile string
		blocked               bool
	}{{"alice", "custom.example", "off", false}, {"bob", "light.example", "light", true}, {"carol", "light.example", "custom", false}} {
		got, err := s.CheckAdBlockDomain(tc.host, "home", tc.client)
		if err != nil {
			t.Fatal(err)
		}
		if got.Blocked != tc.blocked || got.Profile != tc.profile {
			t.Fatalf("diagnostic %s/%s: %+v", tc.client, tc.host, got)
		}
	}
}
