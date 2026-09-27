package service

import (
	"strings"
	"testing"
)

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
