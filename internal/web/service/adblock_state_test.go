package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAdBlockApplicationOnlyChangesForEffectiveRules(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	config := AdBlockSettings{Enabled: true, CustomDomains: "ads.example", Allowlist: "safe.example"}
	if err := s.SaveAdBlockSettings(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	status, _ := s.GetAdBlockApplication()
	if !status.Pending {
		t.Fatal("rule change has no persistent apply intent")
	}
	if err := s.MarkAdBlockApplied(nil); err != nil {
		t.Fatal(err)
	}
	interval := 12
	config.UpdateIntervalHours = &interval
	if err := s.SaveAdBlockSettings(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	status, _ = s.GetAdBlockApplication()
	if status.Pending {
		t.Fatal("interval-only change restarted core")
	}
	config.CustomDomains = "safe.example\nads.example" // raw list changes, effective rules do not
	if err := s.SaveAdBlockSettings(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	status, _ = s.GetAdBlockApplication()
	if status.Pending {
		t.Fatal("allowlisted rule restarted core")
	}
	config.Scope = &AdBlockScope{InboundMode: "include", Inbounds: []string{"test"}, ClientMode: "all"}
	if err := s.SaveAdBlockSettings(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	status, _ = s.GetAdBlockApplication()
	if !status.Pending {
		t.Fatal("scope change was not applied")
	}
	if err := s.MarkAdBlockApplied(fmt.Errorf("core unavailable")); err != nil {
		t.Fatal(err)
	}
	replacement := &SettingService{}
	status, _ = replacement.GetAdBlockApplication()
	if !status.Pending || status.LastError != "core unavailable" || status.NextRetry == "" {
		t.Fatal("restart lost apply failure")
	}
}
func TestAdBlockPauseSurvivesRestartAndResumesWithoutAutoUpdate(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	auto := false
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "ads.example", AutoUpdate: &auto}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkAdBlockApplied(nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAdBlockPause(15); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkAdBlockApplied(nil); err != nil {
		t.Fatal(err)
	}
	replacement := &SettingService{}
	domains, err := replacement.adBlockRuntimeDomains()
	if err != nil || len(domains) != 0 {
		t.Fatal("pause lost on restart")
	}
	if err := saveAdBlockValues(map[string]string{"adBlockPausedUntil": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if err := replacement.ReconcileAdBlockApplication(); err != nil {
		t.Fatal(err)
	}
	domains, err = replacement.adBlockRuntimeDomains()
	if err != nil || len(domains) != 1 {
		t.Fatal("expired pause did not resume")
	}
	status, _ := replacement.GetAdBlockApplication()
	until, _ := replacement.GetAdBlockPausedUntil()
	if !status.Pending || until != "" {
		t.Fatal("resume was not scheduled persistently")
	}
	if err := replacement.SetAdBlockPause(7); err == nil {
		t.Fatal("invalid pause accepted")
	}
}
func TestAdBlockDomainDiagnosisUsesEffectiveRulesAndScope(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	scope := AdBlockScope{InboundMode: "include", Inbounds: []string{"home"}, ClientMode: "exclude", Clients: []string{"alice"}}
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "domain:ads.example\ndomain:googlevideo.com\ndomain:broad.example\nexact.broad.example", Allowlist: "safe.broad.example", YoutubeMode: stringValue("compatible"), Scope: &scope}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		Domain, Inbound, Client string
		Blocked, Context        bool
		Reason                  string
	}{
		{"foo.ads.example", "home", "bob", true, false, "Совпадение"},
		{"foo.ads.example", "home", "alice", false, false, "исключено"},
		{"foo.ads.example", "office", "bob", false, false, "исключено"},
		{"foo.ads.example", "", "", false, true, "Выберите"},
		{"r.googlevideo.com", "home", "bob", false, false, "Исключён"},
		{"foo.broad.example", "home", "bob", false, false, "широкое"},
		{"exact.broad.example", "home", "bob", true, false, "Совпадение"},
	} {
		result, err := s.CheckAdBlockDomain(test.Domain, test.Inbound, test.Client)
		if err != nil || result.Blocked != test.Blocked || result.ContextRequired != test.Context || !strings.Contains(result.Reason, test.Reason) {
			t.Fatalf("%s: %+v %v", test.Domain, result, err)
		}
	}
	for _, invalid := range []string{"https://ads.example/path", "ads.example:443", "*.example", "127.0.0.1", "ads.example\nother.example"} {
		if _, err := s.CheckAdBlockDomain(invalid, "home", "bob"); err == nil {
			t.Fatalf("invalid hostname accepted: %s", invalid)
		}
	}
	if err := s.SetAdBlockPause(5); err != nil {
		t.Fatal(err)
	}
	result, _ := s.CheckAdBlockDomain("foo.ads.example", "home", "bob")
	if result.Blocked || !strings.Contains(result.Reason, "приостановлена") {
		t.Fatal("check ignores pause")
	}
}
