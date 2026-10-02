package singbox

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

const panelClashController = "127.0.0.1:10090"

// MarshalJSON keeps the local Clash controller available for panel-side
// diagnostics while preserving an explicitly configured controller, secret,
// or disabled controller (empty external_controller). It also converts panel
// editor conveniences into the strict runtime sing-box schema.
func (c Config) MarshalJSON() ([]byte, error) {
	type configAlias Config
	copyConfig := configAlias(c)

	outbounds, err := normalizeOutboundsForRuntime(c.Outbounds)
	if err != nil {
		return nil, fmt.Errorf("normalize sing-box outbounds: %w", err)
	}
	copyConfig.Outbounds = outbounds

	route, err := normalizeRouteForRuntime(c.Route)
	if err != nil {
		return nil, fmt.Errorf("normalize sing-box route: %w", err)
	}
	copyConfig.Route = route

	experimental := make(map[string]any, len(c.Experimental)+1)
	for key, value := range c.Experimental {
		experimental[key] = value
	}

	rawClashAPI, exists := experimental["clash_api"]
	if !exists {
		experimental["clash_api"] = map[string]any{
			"external_controller": panelClashController,
		}
		copyConfig.Experimental = experimental
		return json.Marshal(copyConfig)
	}

	configured, ok := rawClashAPI.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("experimental.clash_api must be an object")
	}

	clashAPI := make(map[string]any, len(configured)+1)
	for key, value := range configured {
		clashAPI[key] = value
	}
	controller, exists := clashAPI["external_controller"]
	if !exists {
		clashAPI["external_controller"] = panelClashController
	} else if _, ok := controller.(string); !ok {
		return nil, fmt.Errorf("experimental.clash_api.external_controller must be a string")
	}
	if secret, exists := clashAPI["secret"]; exists {
		if _, ok := secret.(string); !ok {
			return nil, fmt.Errorf("experimental.clash_api.secret must be a string")
		}
	}
	if err := validateClashAPIBindSecurity(clashAPI); err != nil {
		return nil, err
	}

	experimental["clash_api"] = clashAPI
	copyConfig.Experimental = experimental
	return json.Marshal(copyConfig)
}

// validateClashAPIBindSecurity prevents accidentally exposing the Clash REST
// API without authentication. Loopback-only listeners are safe without a
// secret; wildcard, LAN, public and hostname binds require one.
func validateClashAPIBindSecurity(clashAPI map[string]any) error {
	controller, _ := clashAPI["external_controller"].(string)
	controller = strings.TrimSpace(controller)
	if controller == "" {
		return nil
	}

	host, _, err := net.SplitHostPort(controller)
	if err != nil {
		return fmt.Errorf("invalid experimental.clash_api.external_controller %q: %w", controller, err)
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}

	secret, _ := clashAPI["secret"].(string)
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("experimental.clash_api.secret is required when external_controller is not loopback")
	}
	return nil
}
