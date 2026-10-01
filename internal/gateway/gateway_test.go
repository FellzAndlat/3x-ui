package gateway

import (
	"reflect"
	"strings"
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
	if !isXrayGatewayInbound(inbound) {
		t.Fatalf("generated inbound is not recognized as Gateway-owned: %#v", inbound)
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

func TestApplyGatewayConfigIsIdempotent(t *testing.T) {
	cfg := map[string]any{
		"inbounds": []any{
			map[string]any{"tag": "existing-in", "protocol": "socks"},
		},
	}

	if err := applyGatewayConfig(cfg); err != nil {
		t.Fatalf("first applyGatewayConfig() error = %v", err)
	}
	if err := applyGatewayConfig(cfg); err != nil {
		t.Fatalf("second applyGatewayConfig() error = %v", err)
	}

	inbounds, ok := cfg["inbounds"].([]any)
	if !ok {
		t.Fatalf("unexpected inbounds type: %T", cfg["inbounds"])
	}
	gatewayCount := 0
	for _, inbound := range inbounds {
		if isXrayGatewayInbound(inbound) {
			gatewayCount++
		}
	}
	if gatewayCount != 1 {
		t.Fatalf("gateway inbound count = %d, want 1", gatewayCount)
	}
}

func TestApplyGatewayConfigRejectsTagCollision(t *testing.T) {
	cfg := map[string]any{
		"inbounds": []any{
			map[string]any{"tag": inboundTag, "protocol": "socks", "port": float64(1080)},
		},
	}

	err := applyGatewayConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "tag") {
		t.Fatalf("applyGatewayConfig() error = %v, want tag collision", err)
	}
	inbounds := cfg["inbounds"].([]any)
	if len(inbounds) != 1 || itemTag(inbounds[0]) != inboundTag {
		t.Fatalf("conflicting inbound was modified: %#v", inbounds)
	}
	inbound := inbounds[0].(map[string]any)
	if inbound["protocol"] != "socks" || inbound["port"] != float64(1080) {
		t.Fatalf("conflicting inbound contents changed: %#v", inbound)
	}
}

func TestApplyGatewayConfigRejectsPortCollision(t *testing.T) {
	cfg := map[string]any{
		"inbounds": []any{
			map[string]any{"tag": "existing", "protocol": "socks", "port": float64(inboundPort)},
		},
	}

	err := applyGatewayConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("applyGatewayConfig() error = %v, want port collision", err)
	}
	inbounds := cfg["inbounds"].([]any)
	if len(inbounds) != 1 || itemTag(inbounds[0]) != "existing" {
		t.Fatalf("conflicting inbound was modified: %#v", inbounds)
	}
}

func TestGatewayDetectionRequiresOwnedShape(t *testing.T) {
	cfg := map[string]any{
		"inbounds": []any{
			map[string]any{"tag": inboundTag, "protocol": "socks", "port": float64(inboundPort)},
		},
	}
	if hasGatewayInbound(cfg) {
		t.Fatal("hasGatewayInbound() = true for user-owned tag collision")
	}
	if hasGatewayArtifacts(cfg) {
		t.Fatal("hasGatewayArtifacts() = true for user-owned tag collision")
	}
}

func TestRemoveGatewayConfigPreservesTagCollision(t *testing.T) {
	cfg := map[string]any{
		"inbounds": []any{
			map[string]any{"tag": inboundTag, "protocol": "socks", "port": float64(1080)},
		},
	}

	changed, err := removeGatewayConfig(cfg)
	if err != nil {
		t.Fatalf("removeGatewayConfig() error = %v", err)
	}
	if changed {
		t.Fatal("removeGatewayConfig() removed user-owned tag collision")
	}
	inbounds := cfg["inbounds"].([]any)
	if len(inbounds) != 1 || itemTag(inbounds[0]) != inboundTag {
		t.Fatalf("conflicting inbound was modified: %#v", inbounds)
	}
}

func TestLegacyGatewayInboundIsRecognizedAndRemoved(t *testing.T) {
	legacy := map[string]any{
		"listen":   "127.0.0.1",
		"port":     float64(inboundPort),
		"protocol": "dokodemo-door",
		"settings": map[string]any{
			"followRedirect": true,
			"network":        "tcp,udp",
		},
		"streamSettings": map[string]any{
			"sockopt": map[string]any{"tproxy": "tproxy"},
		},
		"tag": inboundTag,
	}
	cfg := map[string]any{"inbounds": []any{legacy}}

	if !hasGatewayInbound(cfg) {
		t.Fatal("legacy Gateway inbound was not recognized")
	}
	changed, err := removeGatewayConfig(cfg)
	if err != nil {
		t.Fatalf("removeGatewayConfig() error = %v", err)
	}
	if !changed {
		t.Fatal("legacy Gateway inbound was not removed")
	}
	if len(cfg["inbounds"].([]any)) != 0 {
		t.Fatalf("legacy Gateway inbound remains: %#v", cfg["inbounds"])
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
	if isLegacyGatewayRule(map[string]any{
		"type":        "field",
		"inboundTag":  []any{inboundTag, "another-inbound"},
		"outboundTag": legacyOutboundTag,
	}) {
		t.Fatal("broader user rule was treated as Gateway-owned")
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
			map[string]any{
				"tag":      legacyOutboundTag,
				"protocol": "freedom",
				"settings": map[string]any{
					"finalRules": []any{map[string]any{"action": "allow"}},
				},
			},
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

func TestRemoveGatewayConfigPreservesLegacyTagCollision(t *testing.T) {
	userOutbound := map[string]any{
		"tag":      legacyOutboundTag,
		"protocol": "freedom",
		"settings": map[string]any{},
	}
	userRule := map[string]any{
		"type":        "field",
		"inboundTag":  []any{inboundTag, "other"},
		"outboundTag": legacyOutboundTag,
	}
	cfg := map[string]any{
		"outbounds": []any{userOutbound},
		"routing":   map[string]any{"rules": []any{userRule}},
	}

	changed, err := removeGatewayConfig(cfg)
	if err != nil {
		t.Fatalf("removeGatewayConfig() error = %v", err)
	}
	if changed {
		t.Fatal("removeGatewayConfig() removed user-owned legacy-tag objects")
	}
	if len(cfg["outbounds"].([]any)) != 1 || len(cfg["routing"].(map[string]any)["rules"].([]any)) != 1 {
		t.Fatalf("user legacy-tag objects changed: %#v", cfg)
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
