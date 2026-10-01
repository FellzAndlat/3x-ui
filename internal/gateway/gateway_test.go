package gateway

import (
	"reflect"
	"testing"
)

func TestApplyAndRemoveGatewayConfigPreservesExistingConfig(t *testing.T) {
	original := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{
			map[string]any{"tag": "existing-in", "protocol": "socks", "port": float64(1080)},
		},
		"outbounds": []any{
			map[string]any{"tag": "existing-out", "protocol": "freedom"},
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{
				map[string]any{"type": "field", "domain": []any{"example.com"}, "outboundTag": "existing-out"},
			},
		},
	}
	cfg := cloneConfig(t, original)

	if err := applyGatewayConfig(cfg); err != nil {
		t.Fatalf("applyGatewayConfig() error = %v", err)
	}
	if !hasGatewayArtifacts(cfg) {
		t.Fatal("Gateway artifacts were not detected after apply")
	}
	if !reflect.DeepEqual(cfg["outbounds"], original["outbounds"]) {
		t.Fatal("Gateway apply changed existing outbounds")
	}
	if !reflect.DeepEqual(cfg["routing"], original["routing"]) {
		t.Fatal("Gateway apply changed existing routing")
	}

	changed, err := removeGatewayConfig(cfg)
	if err != nil {
		t.Fatalf("removeGatewayConfig() error = %v", err)
	}
	if !changed {
		t.Fatal("removeGatewayConfig() reported no changes")
	}
	if hasGatewayArtifacts(cfg) {
		t.Fatal("Gateway artifacts remain after remove")
	}
	if !reflect.DeepEqual(cfg, original) {
		t.Fatalf("unrelated config changed after enable/disable cycle\nwant: %#v\n got: %#v", original, cfg)
	}
}

func TestApplyGatewayConfigUsesTunnelTPROXY(t *testing.T) {
	cfg := map[string]any{}

	if err := applyGatewayConfig(cfg); err != nil {
		t.Fatalf("applyGatewayConfig() error = %v", err)
	}
	inbounds, ok := cfg["inbounds"].([]any)
	if !ok || len(inbounds) != 1 {
		t.Fatalf("unexpected inbounds after apply: %#v", cfg["inbounds"])
	}
	inbound, ok := inbounds[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected gateway inbound: %#v", inbounds[0])
	}
	if inbound["protocol"] != "tunnel" {
		t.Fatalf("gateway protocol = %#v, want tunnel", inbound["protocol"])
	}
	settings, ok := inbound["settings"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected gateway settings: %#v", inbound["settings"])
	}
	if settings["allowedNetwork"] != "tcp,udp" || settings["followRedirect"] != true {
		t.Fatalf("unexpected gateway settings: %#v", settings)
	}
}

func TestApplyGatewayConfigCreatesMissingSections(t *testing.T) {
	cfg := map[string]any{"log": map[string]any{"loglevel": "warning"}}

	if err := applyGatewayConfig(cfg); err != nil {
		t.Fatalf("applyGatewayConfig() error = %v", err)
	}
	if !hasGatewayArtifacts(cfg) {
		t.Fatal("Gateway artifacts were not detected after apply")
	}

	changed, err := removeGatewayConfig(cfg)
	if err != nil {
		t.Fatalf("removeGatewayConfig() error = %v", err)
	}
	if !changed {
		t.Fatal("removeGatewayConfig() reported no changes")
	}
	if hasGatewayArtifacts(cfg) {
		t.Fatal("Gateway artifacts remain after remove")
	}
}

func TestApplyGatewayConfigRejectsMalformedArrays(t *testing.T) {
	cfg := map[string]any{
		"inbounds": map[string]any{"unexpected": true},
	}
	if err := applyGatewayConfig(cfg); err == nil {
		t.Fatal("applyGatewayConfig() accepted non-array inbounds")
	}
}

func TestLegacyGatewayRuleDetectionRequiresOwnedInboundAndOutbound(t *testing.T) {
	if isLegacyGatewayRule(map[string]any{
		"type":        "field",
		"inboundTag":  []any{inboundTag},
		"outboundTag": "another-outbound",
	}) {
		t.Fatal("rule with another outbound was treated as Gateway-owned")
	}
	if !isLegacyGatewayRule(map[string]any{
		"type":        "field",
		"inboundTag":  []any{inboundTag},
		"outboundTag": legacyOutboundTag,
	}) {
		t.Fatal("legacy Gateway routing rule was not detected")
	}
}

func TestRemoveGatewayConfigCleansLegacyArtifacts(t *testing.T) {
	cfg := map[string]any{
		"inbounds": []any{gatewayInbound()},
		"outbounds": []any{
			map[string]any{"tag": legacyOutboundTag, "protocol": "freedom"},
			map[string]any{"tag": "keep", "protocol": "freedom"},
		},
		"routing": map[string]any{
			"rules": []any{
				map[string]any{
					"type":        "field",
					"inboundTag":  []any{inboundTag},
					"outboundTag": legacyOutboundTag,
				},
				map[string]any{"type": "field", "outboundTag": "keep"},
			},
		},
	}

	changed, err := removeGatewayConfig(cfg)
	if err != nil {
		t.Fatalf("removeGatewayConfig() error = %v", err)
	}
	if !changed {
		t.Fatal("removeGatewayConfig() reported no changes")
	}
	if hasGatewayArtifacts(cfg) {
		t.Fatal("Gateway artifacts remain after legacy cleanup")
	}
}

func cloneConfig(t *testing.T, src map[string]any) map[string]any {
	t.Helper()

	clone := make(map[string]any, len(src))
	for key, value := range src {
		clone[key] = cloneValue(value)
	}
	return clone
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, nested := range typed {
			clone[key] = cloneValue(nested)
		}
		return clone
	case []any:
		clone := make([]any, len(typed))
		for index, nested := range typed {
			clone[index] = cloneValue(nested)
		}
		return clone
	default:
		return value
	}
}
