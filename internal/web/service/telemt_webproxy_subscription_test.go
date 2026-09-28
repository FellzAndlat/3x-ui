package service

import (
	"strings"
	"testing"
)

func TestTelemtWebSubscriptionLocationsUseImportedJSONPathAndDisableProxyBuffering(t *testing.T) {
	const backupTemplate = `{
		"users":[{"uuid":"768e8bdd-bee3-4442-9006-b26464148aaa","name":"Imported user","enable":true,"is_active":true}],
		"hconfigs":[{"key":"proxy_path_client","value":"__PROXY_PATH_CLIENT__"}]
	}`

	for _, importedPath := range []string{"ImportedRouteAlpha123", "Different_Route-456"} {
		t.Run(importedPath, func(t *testing.T) {
			backup := strings.Replace(backupTemplate, "__PROXY_PATH_CLIENT__", importedPath, 1)
			parsed, _, err := ParseHiddifyBackup(strings.NewReader(backup))
			if err != nil {
				t.Fatalf("parse imported Hiddify JSON: %v", err)
			}
			alias, err := parsed.HiddifyLegacySubscriptionAlias()
			if err != nil {
				t.Fatalf("read imported subscription path: %v", err)
			}
			if alias.Path != importedPath {
				t.Fatalf("imported subscription path = %q, want %q", alias.Path, importedPath)
			}

			locations := renderTelemtWebSubscriptionLocations(
				[]HiddifyLegacySubscriptionAlias{alias},
				"https://127.0.0.1:2096",
			)

			for _, want := range []string{
				"location ^~ /" + importedPath + "/",
				"proxy_pass https://127.0.0.1:2096;",
				"proxy_http_version 1.1;",
				"proxy_set_header Host $host;",
				"proxy_set_header X-Forwarded-For $remote_addr;",
				"proxy_set_header X-Forwarded-Proto $scheme;",
				"proxy_connect_timeout 5s;",
				"proxy_read_timeout 65s;",
				"proxy_send_timeout 65s;",
				"proxy_request_buffering off;",
				"proxy_buffering off;",
				"proxy_max_temp_file_size 0;",
				"proxy_next_upstream off;",
			} {
				if !strings.Contains(locations, want) {
					t.Fatalf("generated subscription location missing %q:\n%s", want, locations)
				}
			}
		})
	}
}

func TestTelemtWebSubscriptionLocationsFollowChangedSubscriptionPort(t *testing.T) {
	setupConflictDB(t)
	s := &SettingService{}
	if err := s.AddHiddifyLegacySubscriptionAlias(HiddifyLegacySubscriptionAlias{Path: "ImportedRouteAlpha123"}); err != nil {
		t.Fatal(err)
	}
	for _, port := range []string{"443", "2096"} {
		if err := s.setString("subPort", port); err != nil {
			t.Fatal(err)
		}
		locations, err := telemtWebSubscriptionLocations(s)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(locations, "proxy_pass http://127.0.0.1:"+port+";") {
			t.Fatalf("subscription port %s not reflected in Telemt route:\n%s", port, locations)
		}
	}
}
