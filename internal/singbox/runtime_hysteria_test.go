package singbox

import (
	"strings"
	"testing"
)

func TestNormalizeOutboundsForRuntimeRejectsHysteriaPortConflict(t *testing.T) {
	for _, outboundType := range []string{"hysteria", "hysteria2"} {
		t.Run(outboundType, func(t *testing.T) {
			_, err := normalizeOutboundsForRuntime([]map[string]any{{
				"type":         outboundType,
				"tag":          "proxy",
				"server":       "example.com",
				"server_port":  443,
				"server_ports": []string{"443", "8443:8450"},
			}})
			if err == nil || !strings.Contains(err.Error(), "cannot combine server_port with server_ports") {
				t.Fatalf("expected %s port conflict to be rejected, got %v", outboundType, err)
			}
		})
	}
}

func TestNormalizeOutboundsForRuntimeRejectsInvalidHysteria2RealmIPVersion(t *testing.T) {
	_, err := normalizeOutboundsForRuntime([]map[string]any{{
		"type": "hysteria2",
		"tag":  "proxy",
		"realm": map[string]any{
			"ip_version": 5,
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "realm.ip_version") {
		t.Fatalf("expected invalid realm.ip_version to be rejected, got %v", err)
	}
}

func TestNormalizeOutboundsForRuntimeRejectsIPv6Hysteria2RealmPortMapping(t *testing.T) {
	_, err := normalizeOutboundsForRuntime([]map[string]any{{
		"type": "hysteria2",
		"tag":  "proxy",
		"realm": map[string]any{
			"ip_version": 6,
			"port_mapping": map[string]any{
				"enabled": true,
			},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "port_mapping requires IPv4") {
		t.Fatalf("expected IPv6 realm port mapping to be rejected, got %v", err)
	}
}

func TestNormalizeOutboundsForRuntimeAllowsDisabledIPv6Hysteria2RealmPortMapping(t *testing.T) {
	_, err := normalizeOutboundsForRuntime([]map[string]any{{
		"type": "hysteria2",
		"tag":  "proxy",
		"realm": map[string]any{
			"ip_version": 6,
			"port_mapping": map[string]any{
				"enabled": false,
			},
		},
	}})
	if err != nil {
		t.Fatalf("disabled IPv6 realm port mapping should remain a no-op, got %v", err)
	}
}
