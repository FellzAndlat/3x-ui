package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func rawTemplateOutbound(t *testing.T, outbound map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(outbound)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestValidateSingBoxTemplateOutboundAcceptsNativeSSH(t *testing.T) {
	for _, protocol := range []string{"ssh", "singbox:ssh"} {
		t.Run(protocol, func(t *testing.T) {
			outbound := map[string]any{
				"protocol": protocol,
				"tag":      "native-ssh",
				"settings": map[string]any{"server": "ssh.example.com", "user": "proxy"},
			}
			if err := validateSingBoxTemplateOutbound(rawTemplateOutbound(t, outbound), outbound); err != nil {
				t.Fatalf("valid native sing-box outbound rejected: %v", err)
			}
		})
	}
}

func TestValidateSingBoxTemplateOutboundRejectsMalformedNativeProtocol(t *testing.T) {
	outbound := map[string]any{
		"protocol": "singbox:tuic",
		"tag":      "bad-tuic",
		"settings": map[string]any{},
	}
	err := validateSingBoxTemplateOutbound(rawTemplateOutbound(t, outbound), outbound)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "server") {
		t.Fatalf("malformed native TUIC outbound was not rejected correctly: %v", err)
	}
}

func TestValidateSingBoxTemplateOutboundAllowsLegacyDNSPlaceholder(t *testing.T) {
	outbound := map[string]any{"protocol": "dns", "tag": "dns-out", "settings": map[string]any{}}
	if err := validateSingBoxTemplateOutbound(rawTemplateOutbound(t, outbound), outbound); err != nil {
		t.Fatalf("legacy DNS outbound should be consumed by sing-box routing translation: %v", err)
	}
}
