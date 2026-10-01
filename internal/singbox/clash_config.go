package singbox

import (
	"encoding/json"
	"fmt"
	"strings"
)

const panelClashController = "127.0.0.1:10090"

// MarshalJSON keeps the local Clash controller available for panel-side
// diagnostics such as outbound delay checks. User-provided experimental
// settings are preserved, including an explicit controller address or secret.
func (c Config) MarshalJSON() ([]byte, error) {
	type configAlias Config
	copyConfig := configAlias(c)

	experimental := make(map[string]any, len(c.Experimental)+1)
	for key, value := range c.Experimental {
		experimental[key] = value
	}

	clashAPI := map[string]any{}
	if raw, exists := experimental["clash_api"]; exists && raw != nil {
		configured, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("experimental.clash_api must be an object")
		}
		for key, value := range configured {
			clashAPI[key] = value
		}
	}
	if controller, _ := clashAPI["external_controller"].(string); strings.TrimSpace(controller) == "" {
		clashAPI["external_controller"] = panelClashController
	}
	experimental["clash_api"] = clashAPI
	copyConfig.Experimental = experimental

	return json.Marshal(copyConfig)
}
