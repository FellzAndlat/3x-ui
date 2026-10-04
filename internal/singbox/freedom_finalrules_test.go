package singbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func migratedFreedomOutbound() map[string]any {
	return map[string]any{
		"protocol": "freedom",
		"tag":      "direct",
		"settings": map[string]any{
			"domainStrategy": "AsIs",
			"finalRules": []any{
				map[string]any{"action": "block", "ip": []any{"geoip:private"}},
				map[string]any{"action": "allow"},
			},
		},
	}
}

func TestTranslateXrayFreedomMigratedFinalRules(t *testing.T) {
	outbound, err := TranslateXrayOutbound(migratedFreedomOutbound())
	if err != nil {
		t.Fatalf("translate migrated freedom finalRules: %v", err)
	}
	if outbound["type"] != "direct" {
		t.Fatalf("translated type = %v, want direct", outbound["type"])
	}
	if marked, _ := outbound[xrayFreedomPrivateFinalRulesMarker].(bool); !marked {
		t.Fatalf("translated outbound did not preserve the private-egress guard marker: %#v", outbound)
	}
}

func TestTranslateXrayFreedomCustomFinalRulesStillRejected(t *testing.T) {
	raw := migratedFreedomOutbound()
	settings := raw["settings"].(map[string]any)
	settings["finalRules"] = []any{
		map[string]any{"action": "block", "ip": []any{"1.2.3.4"}},
		map[string]any{"action": "allow"},
	}

	_, err := TranslateXrayOutbound(raw)
	if err == nil || !strings.Contains(err.Error(), "finalRules") {
		t.Fatalf("custom finalRules error = %v, want an explicit finalRules rejection", err)
	}
}

func TestPrepareXrayFreedomFinalRulesPreservesSplitRouting(t *testing.T) {
	direct, err := TranslateXrayOutbound(migratedFreedomOutbound())
	if err != nil {
		t.Fatalf("translate direct: %v", err)
	}
	outbounds := []map[string]any{
		direct,
		{"type": "socks", "tag": "proxy"},
	}
	route := map[string]any{
		"final": "direct",
		"rules": []map[string]any{
			{"domain_suffix": []string{"ru"}, "action": "route", "outbound": "direct"},
			{"network": "tcp", "action": "route", "outbound": "proxy"},
		},
	}

	cleaned, guarded, err := prepareXrayFreedomFinalRules(outbounds, route)
	if err != nil {
		t.Fatalf("prepare finalRules: %v", err)
	}
	if _, exists := cleaned[0][xrayFreedomPrivateFinalRulesMarker]; exists {
		t.Fatal("panel-only finalRules marker leaked into the runtime outbound")
	}
	rules, ok := guarded["rules"].([]map[string]any)
	if !ok {
		t.Fatalf("guarded rules type = %T, want []map[string]any", guarded["rules"])
	}
	if len(rules) != 6 {
		t.Fatalf("guarded rule count = %d, want 6: %#v", len(rules), rules)
	}
	if rules[0]["action"] != "resolve" || rules[0]["domain_suffix"] == nil {
		t.Fatalf("explicit direct route is not preceded by a matching resolve rule: %#v", rules[0])
	}
	if rules[1]["action"] != "reject" || rules[1]["type"] != "logical" {
		t.Fatalf("explicit direct route is not preceded by a scoped private-IP reject: %#v", rules[1])
	}
	if rules[2]["action"] != "route" || rules[2]["outbound"] != "direct" {
		t.Fatalf("original direct route moved incorrectly: %#v", rules[2])
	}
	if rules[3]["action"] != "route" || rules[3]["outbound"] != "proxy" {
		t.Fatalf("proxy split route was changed: %#v", rules[3])
	}
	if rules[4]["action"] != "resolve" {
		t.Fatalf("final direct route is not preceded by resolve: %#v", rules[4])
	}
	if rules[5]["action"] != "reject" || rules[5]["ip_is_private"] != true {
		t.Fatalf("final direct route does not reject private IPs: %#v", rules[5])
	}
}

func TestMarshalXrayFreedomFinalRulesMovesGuardIntoRoute(t *testing.T) {
	direct, err := TranslateXrayOutbound(migratedFreedomOutbound())
	if err != nil {
		t.Fatalf("translate direct: %v", err)
	}
	cfg := &Config{
		Outbounds: []map[string]any{direct},
		Route:     map[string]any{"final": "direct"},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal sing-box config: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode marshaled config: %v", err)
	}
	outbounds := decoded["outbounds"].([]any)
	if _, exists := outbounds[0].(map[string]any)[xrayFreedomPrivateFinalRulesMarker]; exists {
		t.Fatalf("panel-only marker leaked into sing-box JSON: %s", data)
	}
	route := decoded["route"].(map[string]any)
	rules := route["rules"].([]any)
	if len(rules) != 2 {
		t.Fatalf("runtime route rules = %d, want resolve + private reject: %s", len(rules), data)
	}
	if rules[0].(map[string]any)["action"] != "resolve" {
		t.Fatalf("first runtime guard is not resolve: %s", data)
	}
	privateReject := rules[1].(map[string]any)
	if privateReject["action"] != "reject" || privateReject["ip_is_private"] != true {
		t.Fatalf("private reject guard missing: %s", data)
	}
}
