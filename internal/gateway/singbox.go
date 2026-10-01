package gateway

import "fmt"

// singBoxGatewayInbound returns the transparent-proxy listener used by
// Gateway Mode when sing-box is the active core. Leaving network unset makes
// sing-box listen for both TCP and UDP, matching the firewall TPROXY rules.
func singBoxGatewayInbound() map[string]any {
	return map[string]any{
		"type":        "tproxy",
		"tag":         inboundTag,
		"listen":      "0.0.0.0",
		"listen_port": inboundPort,
	}
}

// singBoxGatewaySniffRule mirrors Xray Gateway's route-only HTTP/TLS/QUIC
// sniffing. TPROXY receives an IP destination, so sniffing must run before
// domain routing rules in order for those rules to see SNI/Host names.
func singBoxGatewaySniffRule() map[string]any {
	return map[string]any{
		"inbound": []any{inboundTag},
		"action":  "sniff",
		"sniffer": []any{"http", "tls", "quic"},
	}
}

func singBoxGatewayStringSlice(value any) ([]string, bool) {
	switch values := value.(type) {
	case []string:
		return values, true
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			result = append(result, text)
		}
		return result, true
	default:
		return nil, false
	}
}

func isSingBoxGatewaySniffRule(item any) bool {
	rule, ok := item.(map[string]any)
	if !ok || len(rule) != 3 || rule["action"] != "sniff" {
		return false
	}
	inbounds, ok := singBoxGatewayStringSlice(rule["inbound"])
	if !ok || len(inbounds) != 1 || inbounds[0] != inboundTag {
		return false
	}
	sniffers, ok := singBoxGatewayStringSlice(rule["sniffer"])
	if !ok || len(sniffers) != 3 {
		return false
	}
	return sniffers[0] == "http" && sniffers[1] == "tls" && sniffers[2] == "quic"
}

func singBoxGatewayRoute(cfg map[string]any, create bool) (map[string]any, error) {
	value, exists := cfg["route"]
	if !exists || value == nil {
		if !create {
			return nil, nil
		}
		route := map[string]any{}
		cfg["route"] = route
		return route, nil
	}
	route, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("config route section must be an object")
	}
	return route, nil
}

func hasSingBoxGatewaySniffRule(cfg map[string]any) bool {
	route, err := singBoxGatewayRoute(cfg, false)
	if err != nil || route == nil {
		return false
	}
	rules, err := arrayField(route, "rules")
	if err != nil {
		return false
	}
	for _, rule := range rules {
		if isSingBoxGatewaySniffRule(rule) {
			return true
		}
	}
	return false
}

func singBoxPort(value any) (int, bool) {
	switch port := value.(type) {
	case int:
		return port, true
	case int32:
		return int(port), true
	case int64:
		return int(port), true
	case float64:
		converted := int(port)
		if port != float64(converted) {
			return 0, false
		}
		return converted, true
	default:
		return 0, false
	}
}

func isSingBoxGatewayInbound(item any) bool {
	inbound, ok := item.(map[string]any)
	if !ok || itemTag(item) != inboundTag {
		return false
	}
	inboundType, _ := inbound["type"].(string)
	listen, _ := inbound["listen"].(string)
	port, portOK := singBoxPort(inbound["listen_port"])
	return inboundType == "tproxy" && listen == "0.0.0.0" && portOK && port == inboundPort
}

func hasSingBoxGatewayInbound(cfg map[string]any) bool {
	inbounds, ok := cfg["inbounds"].([]any)
	if !ok {
		return false
	}
	for _, item := range inbounds {
		if isSingBoxGatewayInbound(item) {
			return true
		}
	}
	return false
}

func validateSingBoxGatewayConflicts(cfg map[string]any) error {
	inbounds, err := arrayField(cfg, "inbounds")
	if err != nil {
		return err
	}
	gatewayInboundExists := false
	for _, item := range inbounds {
		if isSingBoxGatewayInbound(item) {
			gatewayInboundExists = true
			continue
		}
		if itemTag(item) == inboundTag {
			return fmt.Errorf("sing-box inbound tag %q is already used by a non-Gateway inbound", inboundTag)
		}

		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}
		port, ok := singBoxPort(inbound["listen_port"])
		if !ok || port != inboundPort {
			continue
		}
		tag := itemTag(item)
		if tag == "" {
			tag = "<untagged>"
		}
		return fmt.Errorf("sing-box inbound %q already uses Gateway port %d", tag, inboundPort)
	}

	route, err := singBoxGatewayRoute(cfg, false)
	if err != nil {
		return err
	}
	if route == nil {
		return nil
	}
	rules, err := arrayField(route, "rules")
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if isSingBoxGatewaySniffRule(rule) && !gatewayInboundExists {
			return fmt.Errorf("sing-box Gateway sniff rule already exists without the Gateway inbound")
		}
	}
	return nil
}

func removeSingBoxGatewayConfig(cfg map[string]any) (bool, error) {
	inbounds, err := arrayField(cfg, "inbounds")
	if err != nil {
		return false, err
	}
	route, err := singBoxGatewayRoute(cfg, false)
	if err != nil {
		return false, err
	}
	var rules []any
	if route != nil {
		rules, err = arrayField(route, "rules")
		if err != nil {
			return false, err
		}
	}

	filteredInbounds := make([]any, 0, len(inbounds))
	inboundRemoved := false
	for _, item := range inbounds {
		if isSingBoxGatewayInbound(item) {
			inboundRemoved = true
			continue
		}
		filteredInbounds = append(filteredInbounds, item)
	}
	if inboundRemoved {
		cfg["inbounds"] = filteredInbounds
	}

	ruleRemoved := false
	if route != nil {
		filteredRules := make([]any, 0, len(rules))
		for _, rule := range rules {
			if isSingBoxGatewaySniffRule(rule) {
				ruleRemoved = true
				continue
			}
			filteredRules = append(filteredRules, rule)
		}
		if ruleRemoved {
			if len(filteredRules) == 0 {
				delete(route, "rules")
			} else {
				route["rules"] = filteredRules
			}
		}
	}
	return inboundRemoved || ruleRemoved, nil
}

func applySingBoxGatewayConfig(cfg map[string]any) error {
	if err := validateSingBoxGatewayConflicts(cfg); err != nil {
		return err
	}
	if err := replaceTaggedItem(cfg, "inbounds", inboundTag, singBoxGatewayInbound()); err != nil {
		return err
	}

	route, err := singBoxGatewayRoute(cfg, true)
	if err != nil {
		return err
	}
	rules, err := arrayField(route, "rules")
	if err != nil {
		return err
	}
	// Sniffing has to happen before domain/protocol routing. Replace any
	// Gateway-owned copy and put exactly one rule at the front.
	updated := make([]any, 0, len(rules)+1)
	updated = append(updated, singBoxGatewaySniffRule())
	for _, rule := range rules {
		if !isSingBoxGatewaySniffRule(rule) {
			updated = append(updated, rule)
		}
	}
	route["rules"] = updated
	return nil
}
