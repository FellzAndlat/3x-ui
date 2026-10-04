package singbox

import (
	"fmt"
	"maps"
	"strings"
)

// xrayFreedomPrivateFinalRulesMarker is panel-only metadata carried by a
// translated Xray freedom outbound until Config.MarshalJSON can move the
// protection into sing-box route rules. It must never reach sing-box itself.
const xrayFreedomPrivateFinalRulesMarker = "_xui_xray_freedom_private_final_rules"

// translateXrayFreedomFinalRules recognizes the exact private-egress guard
// installed by the panel migration:
//
//   block geoip:private
//   allow everything else
//
// sing-box direct outbounds do not have an outbound-level finalRules feature.
// Mark the translated outbound so Config.MarshalJSON can preserve the guard in
// route.rules instead of either rejecting it or silently dropping protection.
// Unknown/custom finalRules remain rejected because translating them only
// partially would change administrator policy.
func translateXrayFreedomFinalRules(out, settings map[string]any, tag string) error {
	raw, present := settings["finalRules"]
	if !present || raw == nil {
		return nil
	}
	rules, ok := xrayFreedomFinalRuleObjects(raw)
	if !ok {
		return fmt.Errorf("outbound %q freedom option finalRules has invalid configuration", tag)
	}
	if len(rules) == 0 {
		return nil
	}
	if !isPanelPrivateEgressFinalRules(rules) {
		return fmt.Errorf("outbound %q freedom option finalRules has no equivalent sing-box representation", tag)
	}
	out[xrayFreedomPrivateFinalRulesMarker] = true
	return nil
}

func xrayFreedomFinalRuleObjects(value any) ([]map[string]any, bool) {
	switch rules := value.(type) {
	case []map[string]any:
		return rules, true
	case []any:
		result := make([]map[string]any, 0, len(rules))
		for _, item := range rules {
			rule, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			result = append(result, rule)
		}
		return result, true
	default:
		return nil, false
	}
}

func isPanelPrivateEgressFinalRules(rules []map[string]any) bool {
	if len(rules) != 2 {
		return false
	}
	block, allow := rules[0], rules[1]
	// Keep recognition deliberately narrow. The migration writes exactly these
	// keys; accepting extra matchers (network/port/blockDelay/etc.) would claim
	// equivalence that the route guard below does not provide.
	if len(block) != 2 || len(allow) != 1 {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(rawString(block, "action")), "block") ||
		!strings.EqualFold(strings.TrimSpace(rawString(allow, "action")), "allow") {
		return false
	}
	ips := compatStringSlice(block["ip"])
	return len(ips) == 1 && strings.EqualFold(strings.TrimSpace(ips[0]), "geoip:private")
}

// prepareXrayFreedomFinalRules strips the panel-only outbound marker and moves
// its semantics into route rules. The guard is scoped to traffic that would be
// sent through a protected freedom/direct outbound:
//   - explicit route -> protected outbound: resolve/check immediately before it
//   - protected route.final: resolve/check only after earlier rules did not win
//
// This avoids a global private-IP reject that would incorrectly block traffic
// intentionally routed through another proxy outbound.
func prepareXrayFreedomFinalRules(outbounds []map[string]any, route map[string]any) ([]map[string]any, map[string]any, error) {
	cleaned := make([]map[string]any, 0, len(outbounds))
	protected := make(map[string]struct{})
	for _, source := range outbounds {
		outbound := maps.Clone(source)
		marked, exists := outbound[xrayFreedomPrivateFinalRulesMarker]
		delete(outbound, xrayFreedomPrivateFinalRulesMarker)
		cleaned = append(cleaned, outbound)
		if !exists {
			continue
		}
		enabled, ok := marked.(bool)
		if !ok || !enabled {
			return nil, nil, fmt.Errorf("translated Xray freedom finalRules marker is invalid")
		}
		tag := strings.TrimSpace(rawString(outbound, "tag"))
		if tag == "" {
			return nil, nil, fmt.Errorf("translated Xray freedom finalRules require an outbound tag")
		}
		protected[tag] = struct{}{}
	}
	if len(protected) == 0 {
		return cleaned, route, nil
	}

	guarded := maps.Clone(route)
	if guarded == nil {
		guarded = map[string]any{}
	}
	rules, err := xrayFreedomRouteRuleObjects(guarded["rules"])
	if err != nil {
		return nil, nil, err
	}
	guardedRules := make([]map[string]any, 0, len(rules)+2)
	for _, rule := range rules {
		if isRouteToProtectedFreedom(rule, protected) {
			guardedRules = append(guardedRules, freedomPrivateGuardForRule(rule)...)
		}
		guardedRules = append(guardedRules, rule)
	}

	finalTag := strings.TrimSpace(rawString(guarded, "final"))
	if finalTag == "" && len(cleaned) > 0 {
		// sing-box defaults route.final to the first outbound.
		finalTag = strings.TrimSpace(rawString(cleaned[0], "tag"))
	}
	if _, ok := protected[finalTag]; ok {
		// Xray finalRules resolve domain targets before checking the resulting IP.
		// sing-box's resolve action is non-final, so the following reject can test
		// ip_is_private and ordinary route evaluation then continues for public IPs.
		guardedRules = append(guardedRules,
			map[string]any{"action": "resolve"},
			map[string]any{"ip_is_private": true, "action": "reject"},
		)
	}
	if len(guardedRules) > 0 {
		guarded["rules"] = guardedRules
	}
	return cleaned, guarded, nil
}

func xrayFreedomRouteRuleObjects(value any) ([]map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	switch rules := value.(type) {
	case []map[string]any:
		return append([]map[string]any(nil), rules...), nil
	case []any:
		result := make([]map[string]any, 0, len(rules))
		for i, item := range rules {
			rule, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("route.rules[%d] must be an object", i)
			}
			result = append(result, rule)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("route.rules must be an array")
	}
}

func isRouteToProtectedFreedom(rule map[string]any, protected map[string]struct{}) bool {
	if !strings.EqualFold(strings.TrimSpace(rawString(rule, "action")), "route") {
		return false
	}
	_, ok := protected[strings.TrimSpace(rawString(rule, "outbound"))]
	return ok
}

func freedomPrivateGuardForRule(routeRule map[string]any) []map[string]any {
	matcher := make(map[string]any, len(routeRule))
	for key, value := range routeRule {
		switch key {
		case "action", "outbound":
			continue
		default:
			matcher[key] = value
		}
	}

	resolve := maps.Clone(matcher)
	resolve["action"] = "resolve"
	if len(matcher) == 0 {
		return []map[string]any{
			resolve,
			{"ip_is_private": true, "action": "reject"},
		}
	}
	return []map[string]any{
		resolve,
		{
			"type":   "logical",
			"mode":   "and",
			"rules":  []map[string]any{maps.Clone(matcher), {"ip_is_private": true}},
			"action": "reject",
		},
	}
}
