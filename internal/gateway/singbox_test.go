package gateway

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecodeSingBoxTemplateRejectsNullRoot(t *testing.T) {
	cfg, err := decodeSingBoxTemplate("null")
	if err == nil {
		t.Fatal("decodeSingBoxTemplate() accepted null top-level config")
	}
	if cfg != nil {
		t.Fatalf("decodeSingBoxTemplate() cfg = %#v, want nil", cfg)
	}
	if !strings.Contains(err.Error(), "top-level JSON value must be an object") {
		t.Fatalf("decodeSingBoxTemplate() error = %v, want object validation error", err)
	}
}

func TestDecodeSingBoxTemplateAcceptsObject(t *testing.T) {
	cfg, err := decodeSingBoxTemplate(`{"log":{"level":"info"},"inbounds":[]}`)
	if err != nil {
		t.Fatalf("decodeSingBoxTemplate() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("decodeSingBoxTemplate() returned nil config")
	}
	if _, ok := cfg["log"].(map[string]any); !ok {
		t.Fatalf("decoded log section type = %T, want map[string]any", cfg["log"])
	}
}

func TestApplySingBoxGatewayConfig(t *testing.T) {
	cfg := map[string]any{
		"inbounds": []any{
			map[string]any{"type": "mixed", "tag": "existing", "listen_port": float64(1080)},
		},
	}

	if err := applySingBoxGatewayConfig(cfg); err != nil {
		t.Fatalf("applySingBoxGatewayConfig() error = %v", err)
	}

	inbounds, ok := cfg["inbounds"].([]any)
	if !ok {
		t.Fatalf("inbounds type = %T, want []any", cfg["inbounds"])
	}
	if len(inbounds) != 2 {
		t.Fatalf("inbound count = %d, want 2", len(inbounds))
	}
	if itemTag(inbounds[0]) != "existing" {
		t.Fatalf("existing inbound was not preserved: %#v", inbounds)
	}

	inbound, ok := inbounds[1].(map[string]any)
	if !ok {
		t.Fatalf("gateway inbound type = %T, want map[string]any", inbounds[1])
	}
	if inbound["type"] != "tproxy" {
		t.Fatalf("gateway inbound type = %#v, want tproxy", inbound["type"])
	}
	if inbound["tag"] != inboundTag {
		t.Fatalf("gateway inbound tag = %#v, want %q", inbound["tag"], inboundTag)
	}
	if inbound["listen"] != "0.0.0.0" {
		t.Fatalf("gateway listen = %#v, want 0.0.0.0", inbound["listen"])
	}
	if inbound["listen_port"] != inboundPort {
		t.Fatalf("gateway listen_port = %#v, want %d", inbound["listen_port"], inboundPort)
	}
	if _, exists := inbound["network"]; exists {
		t.Fatalf("gateway network = %#v, want omitted for TCP+UDP", inbound["network"])
	}
	if !hasSingBoxGatewaySniffRule(cfg) {
		t.Fatal("Gateway sniff rule was not added")
	}
}

func TestApplySingBoxGatewayConfigIsIdempotent(t *testing.T) {
	cfg := map[string]any{
		"inbounds": []any{
			map[string]any{"type": "mixed", "tag": "existing"},
		},
	}

	if err := applySingBoxGatewayConfig(cfg); err != nil {
		t.Fatalf("first applySingBoxGatewayConfig() error = %v", err)
	}
	if err := applySingBoxGatewayConfig(cfg); err != nil {
		t.Fatalf("second applySingBoxGatewayConfig() error = %v", err)
	}

	inbounds := cfg["inbounds"].([]any)
	gatewayCount := 0
	for _, inbound := range inbounds {
		if isSingBoxGatewayInbound(inbound) {
			gatewayCount++
		}
	}
	if gatewayCount != 1 {
		t.Fatalf("gateway inbound count = %d, want 1", gatewayCount)
	}
	if len(inbounds) != 2 {
		t.Fatalf("inbound count = %d, want existing + gateway", len(inbounds))
	}

	route := cfg["route"].(map[string]any)
	rules := route["rules"].([]any)
	sniffCount := 0
	for _, rule := range rules {
		if isSingBoxGatewaySniffRule(rule) {
			sniffCount++
		}
	}
	if sniffCount != 1 {
		t.Fatalf("Gateway sniff rule count = %d, want 1", sniffCount)
	}
}

func TestApplySingBoxGatewayConfigPrependsSniffRuleAndPreservesRoute(t *testing.T) {
	first := map[string]any{"domain_suffix": []any{"example.com"}, "action": "route", "outbound": "proxy"}
	second := map[string]any{"ip_is_private": true, "action": "route", "outbound": "direct"}
	cfg := map[string]any{
		"inbounds": []any{},
		"route": map[string]any{
			"final": "proxy",
			"rules": []any{first, second},
		},
	}

	if err := applySingBoxGatewayConfig(cfg); err != nil {
		t.Fatalf("applySingBoxGatewayConfig() error = %v", err)
	}

	route := cfg["route"].(map[string]any)
	if route["final"] != "proxy" {
		t.Fatalf("route final = %#v, want proxy", route["final"])
	}
	rules := route["rules"].([]any)
	if len(rules) != 3 {
		t.Fatalf("route rule count = %d, want 3", len(rules))
	}
	if !isSingBoxGatewaySniffRule(rules[0]) {
		t.Fatalf("first route rule = %#v, want Gateway sniff", rules[0])
	}
	if !reflect.DeepEqual(rules[1], first) || !reflect.DeepEqual(rules[2], second) {
		t.Fatalf("existing route rules were not preserved in order: %#v", rules)
	}
}

func TestApplySingBoxGatewayConfigRejectsOrphanSniffRule(t *testing.T) {
	cfg := map[string]any{
		"inbounds": []any{map[string]any{"type": "mixed", "tag": "existing"}},
		"route":    map[string]any{"rules": []any{singBoxGatewaySniffRule()}},
	}

	err := applySingBoxGatewayConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "sniff rule") {
		t.Fatalf("applySingBoxGatewayConfig() error = %v, want orphan sniff collision", err)
	}
	if len(cfg["inbounds"].([]any)) != 1 {
		t.Fatalf("inbounds changed after rejected config: %#v", cfg["inbounds"])
	}
}

func TestApplySingBoxGatewayConfigRejectsMalformedRouteWithoutMutation(t *testing.T) {
	for name, route := range map[string]any{
		"route object": "invalid",
		"rules array":  map[string]any{"rules": "invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := map[string]any{
				"inbounds": []any{map[string]any{"type": "mixed", "tag": "existing"}},
				"route":    route,
			}
			err := applySingBoxGatewayConfig(cfg)
			if err == nil {
				t.Fatal("applySingBoxGatewayConfig() accepted malformed route")
			}
			inbounds := cfg["inbounds"].([]any)
			if len(inbounds) != 1 || itemTag(inbounds[0]) != "existing" {
				t.Fatalf("inbounds mutated after route validation error: %#v", inbounds)
			}
		})
	}
}

func TestApplySingBoxGatewayConfigRejectsTagCollision(t *testing.T) {
	conflicting := map[string]any{
		"type":        "mixed",
		"tag":         inboundTag,
		"listen":      "127.0.0.1",
		"listen_port": float64(1080),
	}
	cfg := map[string]any{"inbounds": []any{conflicting}}

	err := applySingBoxGatewayConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "tag") {
		t.Fatalf("applySingBoxGatewayConfig() error = %v, want tag collision", err)
	}

	inbounds := cfg["inbounds"].([]any)
	if len(inbounds) != 1 || itemTag(inbounds[0]) != inboundTag {
		t.Fatalf("conflicting inbound was modified: %#v", inbounds)
	}
	inbound := inbounds[0].(map[string]any)
	if inbound["type"] != "mixed" || inbound["listen_port"] != float64(1080) {
		t.Fatalf("conflicting inbound contents changed: %#v", inbound)
	}
}

func TestApplySingBoxGatewayConfigRejectsPortCollision(t *testing.T) {
	conflicting := map[string]any{
		"type":        "mixed",
		"tag":         "existing",
		"listen":      "127.0.0.1",
		"listen_port": float64(inboundPort),
	}
	cfg := map[string]any{"inbounds": []any{conflicting}}

	err := applySingBoxGatewayConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("applySingBoxGatewayConfig() error = %v, want port collision", err)
	}

	inbounds := cfg["inbounds"].([]any)
	if len(inbounds) != 1 || itemTag(inbounds[0]) != "existing" {
		t.Fatalf("conflicting inbound was modified: %#v", inbounds)
	}
	inbound := inbounds[0].(map[string]any)
	if inbound["type"] != "mixed" || inbound["listen_port"] != float64(inboundPort) {
		t.Fatalf("conflicting inbound contents changed: %#v", inbound)
	}
}

func TestSingBoxGatewayDetectionRequiresOwnedShape(t *testing.T) {
	cfg := map[string]any{
		"inbounds": []any{
			map[string]any{
				"type":        "mixed",
				"tag":         inboundTag,
				"listen":      "0.0.0.0",
				"listen_port": float64(inboundPort),
			},
		},
	}
	if hasSingBoxGatewayInbound(cfg) {
		t.Fatal("hasSingBoxGatewayInbound() = true for non-Gateway inbound")
	}
}

func TestRemoveSingBoxGatewayConfigPreservesOtherInbounds(t *testing.T) {
	cfg := map[string]any{
		"inbounds": []any{
			map[string]any{"type": "mixed", "tag": "existing"},
			singBoxGatewayInbound(),
		},
	}

	changed, err := removeSingBoxGatewayConfig(cfg)
	if err != nil {
		t.Fatalf("removeSingBoxGatewayConfig() error = %v", err)
	}
	if !changed {
		t.Fatal("removeSingBoxGatewayConfig() changed = false, want true")
	}

	inbounds := cfg["inbounds"].([]any)
	if len(inbounds) != 1 || itemTag(inbounds[0]) != "existing" {
		t.Fatalf("unrelated inbounds changed: %#v", inbounds)
	}
}

func TestRemoveSingBoxGatewayConfigRemovesSniffAndPreservesUserRoute(t *testing.T) {
	userRule := map[string]any{"domain_suffix": []any{"example.org"}, "action": "route", "outbound": "proxy"}
	cfg := map[string]any{
		"inbounds": []any{singBoxGatewayInbound()},
		"route": map[string]any{
			"final": "proxy",
			"rules": []any{singBoxGatewaySniffRule(), userRule},
		},
	}

	changed, err := removeSingBoxGatewayConfig(cfg)
	if err != nil {
		t.Fatalf("removeSingBoxGatewayConfig() error = %v", err)
	}
	if !changed {
		t.Fatal("removeSingBoxGatewayConfig() changed = false, want true")
	}
	if len(cfg["inbounds"].([]any)) != 0 {
		t.Fatalf("Gateway inbound was not removed: %#v", cfg["inbounds"])
	}
	route := cfg["route"].(map[string]any)
	if route["final"] != "proxy" {
		t.Fatalf("route final changed: %#v", route["final"])
	}
	rules := route["rules"].([]any)
	if len(rules) != 1 || !reflect.DeepEqual(rules[0], userRule) {
		t.Fatalf("user route rules changed: %#v", rules)
	}
}

func TestRemoveSingBoxGatewayConfigPreservesTagCollision(t *testing.T) {
	conflicting := map[string]any{
		"type":        "mixed",
		"tag":         inboundTag,
		"listen":      "0.0.0.0",
		"listen_port": float64(1080),
	}
	cfg := map[string]any{"inbounds": []any{conflicting}}

	changed, err := removeSingBoxGatewayConfig(cfg)
	if err != nil {
		t.Fatalf("removeSingBoxGatewayConfig() error = %v", err)
	}
	if changed {
		t.Fatal("removeSingBoxGatewayConfig() changed = true for user-owned tag collision")
	}
	inbounds := cfg["inbounds"].([]any)
	if len(inbounds) != 1 || itemTag(inbounds[0]) != inboundTag {
		t.Fatalf("conflicting inbound was modified: %#v", inbounds)
	}
	inbound := inbounds[0].(map[string]any)
	if inbound["type"] != "mixed" || inbound["listen_port"] != float64(1080) {
		t.Fatalf("conflicting inbound contents changed: %#v", inbound)
	}
}
