package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func TestAdBlockRuntimeDoesNotMutateStoredTemplate(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	template := `{"routing":{"rules":[{"type":"field","network":"tcp","outboundTag":"direct"}]},"outbounds":[{"tag":"direct","protocol":"freedom"}]}`
	if err := s.saveSetting("xrayTemplateConfig", template); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "ads.example.com\n||track.example^\n"}); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetXrayConfigTemplate()
	if err != nil || stored != template {
		t.Fatalf("template mutated: %s, %v", stored, err)
	}
	cfg := &xray.Config{InboundConfigs: []xray.InboundConfig{{Tag: "client", Sniffing: json_util.RawMessage(`{"enabled":false}`)}}}
	if err := json.Unmarshal([]byte(template), cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.applyXrayAdBlock(cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg.RouterConfig), "full:ads.example.com") || !strings.Contains(string(cfg.RouterConfig), "domain:track.example") {
		t.Fatalf("runtime not blocked: %s", cfg.RouterConfig)
	}
	if !strings.Contains(string(cfg.InboundConfigs[0].Sniffing), `"routeOnly":true`) {
		t.Fatalf("sniffing missing: %s", cfg.InboundConfigs[0].Sniffing)
	}
	if err := s.SetAdBlockEnable(false); err != nil {
		t.Fatal(err)
	}
	var disabled xray.Config
	_ = json.Unmarshal([]byte(template), &disabled)
	if err := s.applyXrayAdBlock(&disabled); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disabled.RouterConfig), adblock.OutboundTag) {
		t.Fatal("disabled filter still present")
	}
}

func TestSingBoxAdBlockRunsAfterNativeRouting(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "ads.example.com\ndomain:track.example\n"}); err != nil {
		t.Fatal(err)
	}
	cfg := singbox.NewConfig()
	cfg.Route["rules"] = []any{map[string]any{"action": "route", "outbound": "direct"}}
	if err := s.applySingBoxAdBlock(cfg); err != nil {
		t.Fatal(err)
	}
	rules := cfg.Route["rules"].([]map[string]any)
	if len(rules) != 3 || rules[0]["action"] != "sniff" || rules[1]["action"] != "reject" || rules[2]["outbound"] != "direct" {
		t.Fatalf("bad order: %+v", rules)
	}
	sets := cfg.Route["rule_set"].([]map[string]any)
	match := sets[0]["rules"].([]map[string]any)[0]
	if match["domain"].([]string)[0] != "ads.example.com" || match["domain_suffix"].([]string)[0] != "track.example" {
		t.Fatalf("bad matchers: %+v", match)
	}
	// Repeated application and native editor round trips cannot duplicate rules.
	if err := s.applySingBoxAdBlock(cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Route["rules"].([]map[string]any)) != 3 {
		t.Fatal("duplicate managed rules")
	}
	if err := s.SetAdBlockEnable(false); err != nil {
		t.Fatal(err)
	}
	if err := s.applySingBoxAdBlock(cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Route["rules"].([]map[string]any)) != 1 || len(cfg.Route["rule_set"].([]map[string]any)) != 0 {
		t.Fatal("disable left managed rules")
	}
	for _, out := range cfg.Outbounds {
		if out["tag"] == adblock.OutboundTag {
			t.Fatal("legacy block outbound generated")
		}
	}
}

func TestAdBlockInvalidSavePreservesSettings(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	previous := AdBlockSettings{Enabled: true, CustomDomains: "ads.example.com", Allowlist: "safe.example"}
	if err := s.SaveAdBlockSettings(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	invalid := 0
	err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: false, CustomDomains: "other.example", Allowlist: "ads.example.com", UpdateIntervalHours: &invalid})
	if err == nil {
		t.Fatal("invalid interval accepted")
	}
	enabled, _ := s.GetAdBlockEnable()
	custom, _ := s.GetAdBlockCustomDomains()
	allowed, _ := s.GetAdBlockAllowlist()
	if !enabled || custom != previous.CustomDomains || allowed != previous.Allowlist {
		t.Fatalf("partial save: %v %s %s", enabled, custom, allowed)
	}
	// A failed source validation must also leave the complete prior state intact.
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, Sources: "http://127.0.0.1/hosts", Allowlist: "ads.example.com"}); err == nil {
		t.Fatal("unsafe source accepted")
	}
	allowed, _ = s.GetAdBlockAllowlist()
	if allowed != previous.Allowlist {
		t.Fatal("failed update changed allowlist")
	}
}

func TestAdBlockRetainsRawRulesForAllowlistChanges(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{
		Enabled: true, CustomDomains: "domain:example.com\nads.example.com", Allowlist: "safe.example.com # site exception",
	}); err != nil {
		t.Fatal(err)
	}
	domains, err := s.adBlockRuntimeDomains()
	if err != nil || strings.Join(domains, ",") != "ads.example.com" {
		t.Fatalf("bad exception: %v, %v", domains, err)
	}
	if err := s.SetAdBlockAllowlist(""); err != nil {
		t.Fatal(err)
	}
	domains, err = s.adBlockRuntimeDomains()
	if err != nil || len(domains) != 2 {
		t.Fatalf("raw rules lost: %v, %v", domains, err)
	}
}

func TestCanceledAdBlockSavePreservesProtection(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "ads.example"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.SaveAdBlockSettings(ctx, AdBlockSettings{Enabled: true, CustomDomains: "replacement.example"}); err == nil {
		t.Fatal("canceled update succeeded")
	}
	raw, _ := s.GetAdBlockDomains()
	if raw != "ads.example" {
		t.Fatalf("canceled update changed list: %s", raw)
	}
}
