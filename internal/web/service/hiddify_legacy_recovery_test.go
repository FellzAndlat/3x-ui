package service

import (
	"strings"
	"testing"
)

func TestHiddifyLegacyAliasesRecoverGeneratedSubURIPath(t *testing.T) {
	setupConflictDB(t)
	s := &SettingService{}

	if err := s.setString(hiddifyLegacySubscriptionAliasesSetting, "[]"); err != nil {
		t.Fatal(err)
	}
	if err := s.setString("subPath", "/sub/"); err != nil {
		t.Fatal(err)
	}
	if err := s.setString("subURI", "https://cdn.vetroff.fun/NvReJ7i2bXWM8kPqdZwz/"); err != nil {
		t.Fatal(err)
	}

	aliases, err := s.GetHiddifyLegacySubscriptionAliases()
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 1 {
		t.Fatalf("aliases = %#v, want one recovered alias", aliases)
	}
	if aliases[0].Path != "NvReJ7i2bXWM8kPqdZwz" {
		t.Fatalf("recovered path = %q", aliases[0].Path)
	}
	if len(aliases[0].Domains) != 1 || aliases[0].Domains[0] != "cdn.vetroff.fun" {
		t.Fatalf("recovered domains = %#v", aliases[0].Domains)
	}

	locations := renderTelemtWebSubscriptionLocations(aliases, "http://127.0.0.1:2096")
	if !strings.Contains(locations, "location ^~ /NvReJ7i2bXWM8kPqdZwz/") {
		t.Fatalf("Telemt route was not recovered from generated subURI:\n%s", locations)
	}
}

func TestHiddifyLegacyAliasesDoNotRecoverRegularSubscriptionPaths(t *testing.T) {
	for _, uri := range []string{
		"https://cdn.example.com/sub/",
		"https://cdn.example.com/subs/",
		"https://cdn.example.com/custom-subscription/",
	} {
		t.Run(uri, func(t *testing.T) {
			setupConflictDB(t)
			s := &SettingService{}
			if err := s.setString(hiddifyLegacySubscriptionAliasesSetting, "[]"); err != nil {
				t.Fatal(err)
			}
			if err := s.setString("subPath", "/sub/"); err != nil {
				t.Fatal(err)
			}
			if err := s.setString("subURI", uri); err != nil {
				t.Fatal(err)
			}
			aliases, err := s.GetHiddifyLegacySubscriptionAliases()
			if err != nil {
				t.Fatal(err)
			}
			if len(aliases) != 0 {
				t.Fatalf("regular subURI %q became a legacy alias: %#v", uri, aliases)
			}
		})
	}
}
