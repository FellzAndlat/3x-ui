package service

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestHiddifyExportRoundTripPreservesClientAndLegacyPath(t *testing.T) {
	const (
		clientID = "b1337b29-8d60-4491-a468-c2bf120cb878"
		alias    = "Different_Route-456"
	)
	expiry := time.Date(2026, time.October, 12, 14, 33, 27, 456000000, time.UTC).UnixMilli()
	records := []model.ClientRecord{{
		Email:        "alice@example.test",
		UUID:         clientID,
		SubID:        clientID,
		TotalGB:      25 * (1 << 30),
		ExpiryTime:   expiry,
		Enable:       true,
		TgID:         12345,
		Comment:      "Alice",
		PrivateKey:   "wg-private",
		PublicKey:    "wg-public",
		PreSharedKey: "wg-psk",
	}}

	backup, stats := buildHiddifyExportBackup(records, HiddifyLegacySubscriptionAlias{
		Path:    alias,
		Domains: []string{"cdn.example.test"},
	}, "https://cdn.example.test/"+alias+"/")
	if stats.Exported != 1 || stats.Skipped != 0 {
		t.Fatalf("export stats = %+v", stats)
	}

	raw, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := ParseHiddifyBackup(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("export is not accepted by Hiddify parser: %v\n%s", err, raw)
	}
	legacy, err := parsed.HiddifyLegacySubscriptionAlias()
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Path != alias {
		t.Fatalf("legacy path = %q, want %q", legacy.Path, alias)
	}
	if len(legacy.Domains) != 1 || legacy.Domains[0] != "cdn.example.test" {
		t.Fatalf("legacy domains = %#v", legacy.Domains)
	}

	clients, err := parsed.HiddifyClients()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 {
		t.Fatalf("round-trip clients = %d", len(clients))
	}
	got := clients[0].Client
	if got.ID != clientID || got.SubID != clientID {
		t.Fatalf("round-trip identity = id:%q sub:%q", got.ID, got.SubID)
	}
	if got.TotalGB != records[0].TotalGB {
		t.Fatalf("round-trip totalGB = %d, want %d", got.TotalGB, records[0].TotalGB)
	}
	if got.ExpiryTime != expiry {
		t.Fatalf("round-trip expiry = %d, want %d", got.ExpiryTime, expiry)
	}
	if !got.Enable || got.TgID != 12345 {
		t.Fatalf("round-trip enable/tg = %v/%d", got.Enable, got.TgID)
	}
	if got.PrivateKey != "wg-private" || got.PublicKey != "wg-public" || got.PreSharedKey != "wg-psk" {
		t.Fatalf("round-trip WireGuard keys were not preserved")
	}
}

func TestHiddifyExportUsesCurrentLegacyAliasAndSkipsInvalidIdentities(t *testing.T) {
	aliases := []HiddifyLegacySubscriptionAlias{
		{Path: "ImportedRouteAlpha123"},
		{Path: "Different_Route-456"},
	}
	selected := selectHiddifyExportAlias(aliases, "https://cdn.example.test/ImportedRouteAlpha123/")
	if selected.Path != "ImportedRouteAlpha123" {
		t.Fatalf("selected path = %q", selected.Path)
	}

	records := []model.ClientRecord{
		{Email: "invalid", UUID: "not-a-uuid", SubID: "also-not-a-uuid", Enable: true},
		{Email: "fallback", SubID: "11111111-2222-3333-4444-555555555555", Enable: true},
		{Email: "duplicate", UUID: "11111111-2222-3333-4444-555555555555", Enable: true},
	}
	backup, stats := buildHiddifyExportBackup(records, selected, "https://cdn.example.test/ImportedRouteAlpha123/")
	if stats.Exported != 1 || stats.Skipped != 2 {
		t.Fatalf("export stats = %+v", stats)
	}
	if len(backup.Users) != 1 || backup.Users[0].UUID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("exported users = %#v", backup.Users)
	}
	if len(backup.HConfigs) != 1 || backup.HConfigs[0].Key != "proxy_path_client" || backup.HConfigs[0].Value != "ImportedRouteAlpha123" {
		t.Fatalf("hconfigs = %#v", backup.HConfigs)
	}
}

func TestHiddifyExportExpiryUnlimited(t *testing.T) {
	days, start := hiddifyExportExpiry(0)
	if days != 0 || start != nil {
		t.Fatalf("unlimited expiry = %d, %#v", days, start)
	}
}
