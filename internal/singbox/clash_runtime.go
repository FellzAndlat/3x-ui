package singbox

import (
	"fmt"
	"net"
	"strings"
)

const panelClashController = "127.0.0.1:10090"

// prepareClashAPIExperimental keeps panel-side diagnostics available while
// preserving explicitly configured Clash API settings. An explicitly empty
// external_controller disables the listener.
func prepareClashAPIExperimental(experimental map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(experimental)+1)
	for key, value := range experimental {
		result[key] = value
	}

	rawClashAPI, exists := result["clash_api"]
	if !exists {
		result["clash_api"] = map[string]any{"external_controller": panelClashController}
		return result, nil
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
	result["clash_api"] = clashAPI
	return result, nil
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
