package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
)

func stringValue(value string) *string { return &value }

func TestYoutubeModesPreserveVideoAndFilterAncillaryAds(t *testing.T) {
	raw := "domain:youtube.com\ndomain:googlevideo.com\nwww.youtube.com\nyoutubei.googleapis.com\nordinary.ads.example"
	for _, mode := range []string{"compatible", "privacy"} {
		domains, err := effectiveAdBlockDomains(raw, "", mode)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(domains, ",")
		for _, forbidden := range []string{"youtube.com", "googlevideo.com", "youtubei.googleapis.com"} {
			if strings.Contains(joined, forbidden) {
				t.Errorf("mode %s breaks video/API: %s", mode, joined)
			}
		}
		if !strings.Contains(joined, "ordinary.ads.example") {
			t.Fatal("unrelated filter removed")
		}
		if (mode == "privacy") != strings.Contains(joined, "googleads.g.doubleclick.net") {
			t.Fatalf("bad ancillary mode %s: %s", mode, joined)
		}
	}
	domains, err := effectiveAdBlockDomains(raw, "", "off")
	if err != nil || !strings.Contains(strings.Join(domains, ","), "domain:googlevideo.com") {
		t.Fatal("off mode changed administrator rules")
	}
	if _, err := effectiveAdBlockDomains(raw, "", "magic-youtube"); err == nil {
		t.Fatal("unsupported mode accepted")
	}
}

func TestAdBlockLocalSettingsUseCachedSources(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	// An unreachable source is deliberate: local changes must use the validated
	// source cache, without requiring a fresh network download.
	sources := "https://unreachable.example/hosts"
	if err := saveAdBlockValues(map[string]string{
		"adBlockSourceCacheReady": "true", "adBlockDownloadedSources": sources,
		"adBlockSourceDomains": "domain:googlevideo.com\noriginal.ads.example", "adBlockSourceCount": "1",
		"adBlockLastUpdate": "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{
		Enabled: true, Sources: sources, CustomDomains: "custom.ads.example", YoutubeMode: stringValue("privacy"),
	}); err != nil {
		t.Fatal(err)
	}
	domains, err := s.adBlockRuntimeDomains()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(domains, ",")
	for _, expected := range []string{"original.ads.example", "custom.ads.example", "googleads.g.doubleclick.net"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("missing %s in %s", expected, joined)
		}
	}
	if strings.Contains(joined, "googlevideo.com") {
		t.Fatal("cached policy did not protect video")
	}
	lastUpdate, _ := s.GetAdBlockLastUpdate()
	if lastUpdate != "2026-01-01T00:00:00Z" {
		t.Fatal("local change falsely marked sources refreshed")
	}
	// Removing a custom rule must not retain it in the source cache.
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, Sources: sources}); err != nil {
		t.Fatal(err)
	}
	raw, _ := s.GetAdBlockDomains()
	if strings.Contains(raw, "custom.ads.example") {
		t.Fatal("removed custom rule remains cached")
	}
}

func TestAdBlockProfilesApplyCanonicalSourcesAndSchedule(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	for _, profile := range AdBlockProfiles() {
		if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Profile: stringValue(profile.ID), Sources: "https://ignored.example/hosts"}); err != nil {
			t.Fatal(err)
		}
		sources, _ := s.GetAdBlockSources()
		interval, _ := s.GetAdBlockUpdateIntervalHours()
		auto, _ := s.GetAdBlockAutoUpdate()
		if sources != profile.Sources || interval != profile.UpdateIntervalHours || !auto {
			t.Fatalf("profile %s was not applied", profile.ID)
		}
	}
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Profile: stringValue("unknown")}); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestAdBlockRetryBackoffAndSuccessfulReset(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	for count, delay := range []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, 80 * time.Minute, 160 * time.Minute, 320 * time.Minute, 6 * time.Hour} {
		if got := adBlockRetryDelay(count + 1); got != delay {
			t.Fatalf("failure %d: got %s want %s", count+1, got, delay)
		}
	}
	if err := s.recordAdBlockFailure(fmt.Errorf("download failed")); err != nil {
		t.Fatal(err)
	}
	status, err := s.GetAdBlockAutomationStatus()
	if err != nil || status.RetryCount != 1 || status.LastError != "download failed" {
		t.Fatalf("bad diagnostics: %+v %v", status, err)
	}
	due, err := s.AdBlockRetryDue(time.Now().UTC())
	if err != nil || due {
		t.Fatal("retry ignored backoff")
	}
	retry, _ := time.Parse(time.RFC3339, status.NextRetry)
	due, err = s.AdBlockRetryDue(retry)
	if err != nil || !due {
		t.Fatal("retry not due at deadline")
	}
	result := &AdBlockUpdateResult{UpdatedAt: time.Now().UTC().Format(time.RFC3339), DomainCount: 1, SourceDomains: "ads.example"}
	if err := saveAdBlockValues(adBlockSuccessfulUpdateValues(result, "ads.example", "")); err != nil {
		t.Fatal(err)
	}
	status, _ = s.GetAdBlockAutomationStatus()
	if status.LastError != "" || status.RetryCount != 0 || status.NextRetry != "" {
		t.Fatal("successful update did not clear failure state")
	}
}

func TestAdBlockMaintainedSourceFixtures(t *testing.T) {
	for _, test := range []struct{ profile, env string }{
		{"light", "XUI_ADBLOCK_OISD_SMALL_FIXTURE"}, {"strict", "XUI_ADBLOCK_OISD_BIG_FIXTURE"}, {"mobile", "XUI_ADBLOCK_ADGUARD_FIXTURE"},
	} {
		t.Run(test.profile, func(t *testing.T) {
			path := os.Getenv(test.env)
			if path == "" {
				t.Skip("set " + test.env + " to check a downloaded maintained source")
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			domains, err := adblock.ParseDomains(f, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(domains) < 100 || len(domains) > adblock.DefaultMaxDomains {
				t.Fatalf("invalid source: %d entries", len(domains))
			}
			t.Logf("parsed %d supported entries", len(domains))
		})
	}
}

func TestAdBlockFailedRefreshPreservesCachedRulesAndRecordsRetry(t *testing.T) {
	setupBulkDB(t)
	s := &SettingService{}
	if err := s.SaveAdBlockSettings(context.Background(), AdBlockSettings{Enabled: true, CustomDomains: "ads.example"}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetAdBlockDomains()
	updated, _ := s.GetAdBlockLastUpdate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.UpdateAdBlock(ctx); err == nil {
		t.Fatal("canceled refresh accepted")
	}
	after, _ := s.GetAdBlockDomains()
	refreshed, _ := s.GetAdBlockLastUpdate()
	status, err := s.GetAdBlockAutomationStatus()
	if err != nil || before != after || updated != refreshed || status.LastError == "" || status.RetryCount != 1 || status.NextRetry == "" {
		t.Fatalf("failed refresh corrupted state: %+v, %v", status, err)
	}
}
