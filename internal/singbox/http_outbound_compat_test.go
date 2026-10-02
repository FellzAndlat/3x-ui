package singbox

import (
	"reflect"
	"testing"
)

func TestTranslateXrayHTTPOutboundPreservesHeaders(t *testing.T) {
	raw := map[string]any{
		"protocol": "http",
		"tag":      "corp-http",
		"settings": map[string]any{
			"servers": []any{map[string]any{
				"address": "proxy.example.com",
				"port":    8080,
				"users": []any{map[string]any{
					"user": "alice",
					"pass": "secret",
				}},
			}},
			"headers": map[string]any{
				"X-Proxy-Token": "token",
				"User-Agent":    "3x-ui",
			},
		},
	}

	got, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	wantHeaders := map[string]any{
		"X-Proxy-Token": "token",
		"User-Agent":    "3x-ui",
	}
	if !reflect.DeepEqual(got["headers"], wantHeaders) {
		t.Fatalf("HTTP headers = %#v, want %#v", got["headers"], wantHeaders)
	}

	// Translation must not retain a mutable reference to the Xray config.
	wantHeaders["X-Proxy-Token"] = "changed"
	if gotHeaders := got["headers"].(map[string]any); gotHeaders["X-Proxy-Token"] != "token" {
		t.Fatalf("translated headers changed through source alias: %#v", gotHeaders)
	}
}
