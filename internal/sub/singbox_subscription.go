package sub

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
)

// buildSeparatedSingBoxSubscription emits one native sing-box document per
// subscription proxy for direct sing-box imports.
func buildSeparatedSingBoxSubscription(template map[string]any, proxies []map[string]any, alwaysReturnArray bool) (string, error) {
	if len(proxies) == 0 {
		return "", nil
	}
	var translatedDNS, translatedRoute map[string]any
	if template != nil {
		if rawDNS, ok := template["dns"].(map[string]any); ok {
			var err error
			translatedDNS, err = singbox.TranslateXrayDNS(rawDNS)
			if err != nil {
				return "", fmt.Errorf("%w: translate DNS: %w", errSubscriptionFormatUnsupported, err)
			}
		}
		if rawRouting, ok := template["routing"].(map[string]any); ok {
			var err error
			translatedRoute, err = singbox.TranslateXrayRouting(rawRouting)
			if err != nil {
				return "", fmt.Errorf("%w: translate routing: %w", errSubscriptionFormatUnsupported, err)
			}
		}
	}
	// A standalone profile must be able to resolve the proxy's server even
	// when the panel has no custom DNS template. Keep the local resolver
	// separate from the user's DNS rule and final server.
	if translatedDNS == nil {
		translatedDNS = map[string]any{}
	}
	servers, _ := translatedDNS["servers"].([]map[string]any)
	resolverTag := "panel-local"
	usedTags := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		if tag, ok := server["tag"].(string); ok {
			usedTags[tag] = struct{}{}
		}
	}
	for suffix := 1; ; suffix++ {
		if _, exists := usedTags[resolverTag]; !exists {
			break
		}
		resolverTag = fmt.Sprintf("panel-local-%d", suffix)
	}
	servers = append(servers, map[string]any{"type": "local", "tag": resolverTag})
	translatedDNS["servers"] = servers
	if translatedRoute == nil {
		translatedRoute = map[string]any{}
	}
	translatedRoute["default_domain_resolver"] = resolverTag

	configs := make([]json.RawMessage, 0, len(proxies))
	for _, proxy := range proxies {
		if proxy == nil {
			continue
		}
		proxyTag, _ := proxy["tag"].(string)
		if proxyTag == "" {
			return "", fmt.Errorf("%w: sing-box proxy has no tag", errSubscriptionFormatUnsupported)
		}
		var transport map[string]any
		if value, ok := proxy["_panel_shadowtls_transport"].(map[string]any); ok {
			transport = value
			proxy = maps.Clone(proxy)
			delete(proxy, "_panel_shadowtls_transport")
			if transport["tag"] == proxyTag {
				return "", fmt.Errorf("%w: ShadowTLS transport tag conflicts with proxy tag", errSubscriptionFormatUnsupported)
			}
		}
		directTag, blockedTag := "direct", "blocked"
		if proxyTag == directTag {
			directTag = "panel-direct"
		}
		if proxyTag == blockedTag {
			blockedTag = "panel-blocked"
		}
		route, err := bindSingBoxProfileRoute(translatedRoute, proxyTag, directTag, blockedTag)
		if err != nil {
			return "", fmt.Errorf("%w: %w", errSubscriptionFormatUnsupported, err)
		}

		outbounds := []any{proxy}
		if transport != nil {
			outbounds = append(outbounds, transport)
		}
		outbounds = append(outbounds, map[string]any{"type": "direct", "tag": directTag}, map[string]any{"type": "block", "tag": blockedTag})
		cfg := map[string]any{
			"$schema": "https://sing-box.sagernet.org/schema.json",
			"outbounds": outbounds,
		}

		cfg["dns"] = translatedDNS
		cfg["route"] = route

		encoded, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal sing-box subscription profile: %w", err)
		}
		configs = append(configs, encoded)
	}

	if len(configs) == 0 {
		return "", nil
	}
	if len(configs) == 1 && !alwaysReturnArray {
		return string(configs[0]), nil
	}

	encoded, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal sing-box subscription array: %w", err)
	}
	return string(encoded), nil
}

// Hiddify's JSON importer expects one object with outbounds, not an array of
// full profiles. Give each ShadowTLS transport a unique hidden tag so only its
// inner Shadowsocks proxy appears in the selector.
func buildHiddifySingBoxSubscription(proxies []map[string]any) (string, error) {
	if len(proxies) == 0 {
		return "", nil
	}
	outbounds := make([]any, 0, len(proxies)*2)
	for i, original := range proxies {
		proxy := maps.Clone(original)
		if transport, ok := proxy["_panel_shadowtls_transport"].(map[string]any); ok {
			transport = maps.Clone(transport)
			tag := fmt.Sprintf("§hide§ shadowtls-transport-%d", i+1)
			transport["tag"] = tag
			proxy["detour"] = tag
			delete(proxy, "_panel_shadowtls_transport")
			outbounds = append(outbounds, transport)
		}
		outbounds = append(outbounds, proxy)
	}
	encoded, err := json.MarshalIndent(map[string]any{"outbounds": outbounds}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal Hiddify sing-box subscription: %w", err)
	}
	return string(encoded), nil
}

func bindSingBoxProfileRoute(template map[string]any, proxyTag, directTag, blockedTag string) (map[string]any, error) {
	route := maps.Clone(template)
	resolve := func(tag string) (string, error) {
		switch tag {
		case "proxy":
			return proxyTag, nil
		case "direct":
			return directTag, nil
		case "block", "blocked":
			return blockedTag, nil
		default:
			return "", fmt.Errorf("sing-box profile cannot route to outbound %q", tag)
		}
	}
	if final, ok := route["final"].(string); ok && final != "" {
		tag, err := resolve(final)
		if err != nil {
			return nil, err
		}
		route["final"] = tag
	}
	if original, ok := route["rules"].([]map[string]any); ok {
		rules := make([]map[string]any, 0, len(original))
		for _, rule := range original {
			copy := maps.Clone(rule)
			if target, ok := copy["outbound"].(string); ok {
				tag, err := resolve(target)
				if err != nil {
					return nil, err
				}
				copy["outbound"] = tag
			}
			rules = append(rules, copy)
		}
		route["rules"] = rules
	}
	return route, nil
}
