package service

import (
	"strings"
	"testing"
)

func TestTelemtWebSubscriptionLocationsDisableProxyBuffering(t *testing.T) {
	locations := renderTelemtWebSubscriptionLocations([]HiddifyLegacySubscriptionAlias{
		{Path: "NvReJ7i2bXWM8kPqdZwz"},
	}, "https://127.0.0.1:2096")

	for _, want := range []string{
		"location ^~ /NvReJ7i2bXWM8kPqdZwz/",
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
}
