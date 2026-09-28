package service

import (
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestHiddifyClientsPreserveIndependentUserURLs(t *testing.T) {
	const backup = `{
		"users":[
			{"uuid":"768e8bdd-bee3-4442-9006-b26464148aaa","name":"Alice","enable":true,"is_active":true},
			{"uuid":"bb9f1752-1aba-4638-8ca5-0ee5e3948a88","name":"Bob","enable":true,"is_active":true}
		],
		"domains":[{"domain":"old.example.com"}],
		"hconfigs":[{"key":"proxy_path_client","value":"SharedPath123"}]
	}`
	parsed, preview, err := ParseHiddifyBackup(strings.NewReader(backup))
	if err != nil {
		t.Fatalf("parse Hiddify backup: %v", err)
	}
	if preview.Users != 2 {
		t.Fatalf("preview users = %d, want 2", preview.Users)
	}
	// Mapping users must work before a database exists. Saving the shared URL
	// belongs to the import step after the clients have been persisted.
	clients, err := parsed.HiddifyClients()
	if err != nil {
		t.Fatalf("map Hiddify users: %v", err)
	}
	if len(clients) != 2 {
		t.Fatalf("mapped users = %d, want 2", len(clients))
	}
	for i, id := range []string{"768e8bdd-bee3-4442-9006-b26464148aaa", "bb9f1752-1aba-4638-8ca5-0ee5e3948a88"} {
		client := clients[i]
		if client.Client.ID != id || client.Client.SubID != id || client.Client.Password != id || client.Client.Auth != id {
			t.Fatalf("user %d lost their UUID identity", i)
		}
		if len(client.InboundIds) != 0 || strings.Contains(client.Client.Email, "old.example.com") {
			t.Fatalf("user %d was tied to an old domain or inbound", i)
		}
	}
	alias, err := parsed.HiddifyLegacySubscriptionAlias()
	if err != nil || alias.Path != "SharedPath123" {
		t.Fatalf("shared URL path = %#v, %v", alias, err)
	}
}

func TestHiddifyImportSkipsUnusedDefaultButKeepsLastUser(t *testing.T) {
	const backup = `{"users":[
		{"uuid":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","name":"default","enable":true,"is_active":true,"start_date":null,"last_online":"0001-01-01 00:00:00","current_usage_GB":0},
		{"uuid":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","name":"default","enable":true,"is_active":true,"start_date":"2026-01-01","last_online":"2026-09-01 12:00:00","current_usage_GB":12},
		{"uuid":"cccccccc-cccc-4ccc-8ccc-cccccccccccc","name":"Vadlo","enable":true,"is_active":true,"start_date":"2026-07-20","last_online":"2026-09-13 01:12:17","current_usage_GB":5}
	]}`
	parsed, preview, err := ParseHiddifyBackup(strings.NewReader(backup))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Users != 2 || !strings.Contains(strings.Join(preview.Warnings, " "), "пропущено: 1") {
		t.Fatalf("preview = %+v, want two users and a skipped placeholder warning", preview)
	}
	clients, err := parsed.HiddifyClients()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 2 || clients[0].Client.SubID != "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" || clients[1].Client.SubID != "cccccccc-cccc-4ccc-8ccc-cccccccccccc" {
		t.Fatalf("import lost a real default account or the last user: %+v", clients)
	}
}

func TestHiddifySubscriptionURLUsesBackupPathAndPublicDomain(t *testing.T) {
	s := &SettingService{}
	for _, path := range []string{"SharedPath123", "Different_Path-456"} {
		alias := HiddifyLegacySubscriptionAlias{Path: path, Domains: []string{"old.example.com"}}
		got, err := s.HiddifySubscriptionURI(alias, "cdn.example.com")
		if err != nil {
			t.Fatal(err)
		}
		if want := "https://cdn.example.com/" + path + "/"; got != want {
			t.Fatalf("subscription base = %q, want %q", got, want)
		}
	}
	if got, err := s.HiddifySubscriptionURI(HiddifyLegacySubscriptionAlias{Path: "BackupPath123"}, "http://cdn.example.com:2096"); err != nil || got != "https://cdn.example.com/BackupPath123/" {
		t.Fatalf("imported URL must use HTTPS on 443: %q, %v", got, err)
	}
	if got, err := s.HiddifySubscriptionURI(HiddifyLegacySubscriptionAlias{Path: "BackupPath123"}, "https://cdn.example.com:2096"); err != nil || got != "https://cdn.example.com/BackupPath123/" {
		t.Fatalf("old HTTPS URL must drop the listener port: %q, %v", got, err)
	}
	for _, input := range []string{"https://cdn.example.com/wrong-path", "file://cdn.example.com", "https://user:pass@cdn.example.com"} {
		if _, err := s.HiddifySubscriptionURI(HiddifyLegacySubscriptionAlias{Path: "SharedPath123"}, input); err == nil {
			t.Fatalf("accepted invalid public origin %q", input)
		}
	}
}

func TestSaveHiddifySubscriptionURLAffectsImportedUsersOnly(t *testing.T) {
	setupConflictDB(t)
	s := &SettingService{}
	alias := HiddifyLegacySubscriptionAlias{Path: "BackupPath123", Domains: []string{"old.example.com"}}
	const importedID = "768e8bdd-bee3-4442-9006-b26464148aaa"
	imported := model.ClientRecord{Email: "hiddify_Alice_768e8bdd", UUID: importedID, SubID: importedID, Group: "Hiddify"}
	regular := model.ClientRecord{Email: "ordinary@example.com", SubID: "ordinary", UUID: "ordinary"}
	if err := database.GetDB().Create(&imported).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&regular).Error; err != nil {
		t.Fatal(err)
	}
	uri, err := s.HiddifySubscriptionURI(alias, "https://cdn.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveHiddifySubscriptionURL(alias, uri, []ClientCreatePayload{{Client: model.Client{Email: imported.Email, ID: importedID, SubID: importedID}}}); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetSubURI()
	if err != nil || stored != "" {
		t.Fatalf("global subscription base changed to %q: %v", stored, err)
	}
	urls, err := s.GetHiddifySubscriptionURIs()
	if err != nil || urls[importedID] != "https://cdn.example.com/BackupPath123/" || urls[regular.SubID] != "" {
		t.Fatalf("per-user subscription URLs = %#v, %v", urls, err)
	}
	aliases, err := s.GetHiddifyLegacySubscriptionAliases()
	if err != nil || len(aliases) != 1 || aliases[0].Path != alias.Path {
		t.Fatalf("legacy routes = %#v, %v", aliases, err)
	}
}

func TestMigrateHiddifySubscriptionURLsRestoresGlobalURL(t *testing.T) {
	setupConflictDB(t)
	s := &SettingService{}
	const importedID = "768e8bdd-bee3-4442-9006-b26464148aaa"
	for _, rec := range []model.ClientRecord{
		{Email: "hiddify_Alice_768e8bdd", UUID: importedID, SubID: importedID, Group: "Hiddify"},
		{Email: "ordinary@example.com", UUID: "ordinary", SubID: "ordinary"},
	} {
		if err := database.GetDB().Create(&rec).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddHiddifyLegacySubscriptionAlias(HiddifyLegacySubscriptionAlias{Path: "BackupPath123"}); err != nil {
		t.Fatal(err)
	}
	// The old saved /subs/ address can differ from the current listener path.
	if err := s.setString("subPath", "/sub/"); err != nil {
		t.Fatal(err)
	}
	if err := s.setString("subPort", "2096"); err != nil {
		t.Fatal(err)
	}
	if err := s.setString("subURI", "https://cdn.example.com:2096/BackupPath123/"); err != nil {
		t.Fatal(err)
	}
	count, err := s.MigrateHiddifySubscriptionURLs()
	if err != nil || count != 1 {
		t.Fatalf("migrated users = %d, %v", count, err)
	}
	stored, err := s.GetSubURI()
	if err != nil || stored != "" {
		t.Fatalf("global URL = %q, %v", stored, err)
	}
	urls, err := s.GetHiddifySubscriptionURIs()
	if err != nil || urls[importedID] != "https://cdn.example.com/BackupPath123/" || urls["ordinary"] != "" {
		t.Fatalf("per-user URLs = %#v, %v", urls, err)
	}
	if count, err := s.MigrateHiddifySubscriptionURLs(); err != nil || count != 0 {
		t.Fatalf("second migration = %d, %v", count, err)
	}
}

func TestMigrateHiddifySubscriptionURLPreservesCustomPath(t *testing.T) {
	setupConflictDB(t)
	s := &SettingService{}
	if err := s.AddHiddifyLegacySubscriptionAlias(HiddifyLegacySubscriptionAlias{Path: "BackupPath123"}); err != nil {
		t.Fatal(err)
	}
	if err := s.setString("subURI", "https://cdn.example.com/custom/"); err != nil {
		t.Fatal(err)
	}
	count, err := s.MigrateHiddifySubscriptionURLs()
	if err != nil || count != 0 {
		t.Fatalf("custom URL migrated: %d, %v", count, err)
	}
}

func TestParseHiddifyBackupReadsLegacySubscriptionPathFromBackup(t *testing.T) {
	const backupTemplate = `{
		"users":[{"uuid":"768e8bdd-bee3-4442-9006-b26464148aaa","name":"Vadlo","enable":true,"is_active":true,"usage_limit_GB":100,"current_usage_GB":10,"package_days":30}],
		"proxies":[{"enable":true,"proto":"vless","transport":"tcp","l3":"reality","cdn":"direct"}],
		"domains":[{"domain":"vetroff.fun","download_domain":"","show_domains":["vetroff.fun","sub.vetroff.fun"]}],
		"hconfigs":[{"key":"proxy_path_client","value":"__PROXY_PATH_CLIENT__"}]
	}`

	for _, proxyPath := range []string{"BackupPathAlpha123", "Another_Backup-Path456"} {
		t.Run(proxyPath, func(t *testing.T) {
			backup := strings.Replace(backupTemplate, "__PROXY_PATH_CLIENT__", proxyPath, 1)
			parsed, preview, err := ParseHiddifyBackup(strings.NewReader(backup))
			if err != nil {
				t.Fatalf("ParseHiddifyBackup: %v", err)
			}
			alias, err := parsed.HiddifyLegacySubscriptionAlias()
			if err != nil {
				t.Fatalf("HiddifyLegacySubscriptionAlias: %v", err)
			}
			if alias.Path != proxyPath {
				t.Fatalf("legacy path = %q, want value from backup %q", alias.Path, proxyPath)
			}
			if len(alias.Domains) != 2 || alias.Domains[0] != "vetroff.fun" || alias.Domains[1] != "sub.vetroff.fun" {
				t.Fatalf("legacy domains = %#v", alias.Domains)
			}
			if len(preview.Warnings) == 0 || !strings.Contains(preview.Warnings[0], proxyPath) || !strings.Contains(preview.Warnings[0], "любом домене") {
				t.Fatalf("preview warnings = %#v", preview.Warnings)
			}
		})
	}
}

func TestParseHiddifyBackupReadsProxyPathFromAlternativeLayouts(t *testing.T) {
	tests := []struct {
		name   string
		backup string
		want   string
	}{
		{
			name: "hconfigs object",
			backup: `{
				"users":[{"uuid":"768e8bdd-bee3-4442-9006-b26464148aaa","enable":true,"is_active":true}],
				"hconfigs":{"proxy_path_client":"AnotherSecretPath01"}
			}`,
			want: "AnotherSecretPath01",
		},
		{
			name: "nested settings",
			backup: `{
				"users":[{"uuid":"bb9f1752-1aba-4638-8ca5-0ee5e3948a88","enable":true,"is_active":true}],
				"backup_meta":{"settings":{"proxy_path_client":"PerBackupPath_02"}}
			}`,
			want: "PerBackupPath_02",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, _, err := ParseHiddifyBackup(strings.NewReader(tt.backup))
			if err != nil {
				t.Fatalf("ParseHiddifyBackup: %v", err)
			}
			alias, err := parsed.HiddifyLegacySubscriptionAlias()
			if err != nil {
				t.Fatalf("HiddifyLegacySubscriptionAlias: %v", err)
			}
			if alias.Path != tt.want {
				t.Fatalf("legacy path = %q, want %q", alias.Path, tt.want)
			}
			if tt.name == "hconfigs object" {
				value, found, err := parsed.hiddifyConfigString("proxy_path_client")
				if err != nil || !found || value != tt.want {
					t.Fatalf("object settings were not decoded: value %q, found %v, error %v", value, found, err)
				}
			}
		})
	}
}

func TestParseHiddifyBackupAcceptsUsersOnly(t *testing.T) {
	const backup = `{
		"users":[{"uuid":"768e8bdd-bee3-4442-9006-b26464148aaa","name":"Vadlo","enable":true,"is_active":true,"usage_limit_GB":100,"current_usage_GB":10,"package_days":30}]
	}`

	parsed, preview, err := ParseHiddifyBackup(strings.NewReader(backup))
	if err != nil {
		t.Fatalf("ParseHiddifyBackup users-only: %v", err)
	}
	if preview.Users != 1 {
		t.Fatalf("preview users = %d", preview.Users)
	}
	alias, err := parsed.HiddifyLegacySubscriptionAlias()
	if err != nil {
		t.Fatalf("HiddifyLegacySubscriptionAlias: %v", err)
	}
	if alias.Path != "" || len(alias.Domains) != 0 {
		t.Fatalf("unexpected legacy alias = %#v", alias)
	}
}

func TestParseHiddifyBackupAcceptsUsersAndLegacyPathWithoutDomains(t *testing.T) {
	const backup = `{
		"users":[{"uuid":"768e8bdd-bee3-4442-9006-b26464148aaa","name":"Vadlo","enable":true,"is_active":true}],
		"hconfigs":[{"key":"proxy_path_client","value":"Client_Path-1"}]
	}`

	parsed, _, err := ParseHiddifyBackup(strings.NewReader(backup))
	if err != nil {
		t.Fatalf("ParseHiddifyBackup partial: %v", err)
	}
	alias, err := parsed.HiddifyLegacySubscriptionAlias()
	if err != nil {
		t.Fatalf("HiddifyLegacySubscriptionAlias: %v", err)
	}
	if alias.Path != "Client_Path-1" || len(alias.Domains) != 0 {
		t.Fatalf("legacy alias = %#v", alias)
	}
}

func TestParseHiddifyBackupRejectsUnsafeLegacyPath(t *testing.T) {
	const backup = `{
		"users":[{"uuid":"768e8bdd-bee3-4442-9006-b26464148aaa","enable":true,"is_active":true}],
		"proxies":[{"enable":true,"proto":"vless","transport":"tcp","l3":"reality","cdn":"direct"}],
		"domains":[{"domain":"vetroff.fun"}],
		"hconfigs":[{"key":"proxy_path_client","value":"../admin"}]
	}`

	if _, _, err := ParseHiddifyBackup(strings.NewReader(backup)); err == nil {
		t.Fatal("expected unsafe proxy_path_client to be rejected")
	}
}

func TestParseHiddifyBackupRejectsConflictingProxyPaths(t *testing.T) {
	const backup = `{
		"users":[{"uuid":"768e8bdd-bee3-4442-9006-b26464148aaa","enable":true,"is_active":true}],
		"hconfigs":[{"key":"proxy_path_client","value":"PathOne"}],
		"settings":{"proxy_path_client":"PathTwo"}
	}`

	if _, _, err := ParseHiddifyBackup(strings.NewReader(backup)); err == nil {
		t.Fatal("expected conflicting proxy_path_client values to be rejected")
	}
}

func TestNormalizeHiddifyLegacySubscriptionAlias(t *testing.T) {
	alias, err := normalizeHiddifyLegacySubscriptionAlias(HiddifyLegacySubscriptionAlias{
		Path:    "/Client_Path-1/",
		Domains: []string{"Example.COM.", "example.com", "bad/domain"},
	})
	if err != nil {
		t.Fatalf("normalize alias: %v", err)
	}
	if alias.Path != "Client_Path-1" {
		t.Fatalf("path = %q", alias.Path)
	}
	if len(alias.Domains) != 1 || alias.Domains[0] != "example.com" {
		t.Fatalf("domains = %#v", alias.Domains)
	}
}
