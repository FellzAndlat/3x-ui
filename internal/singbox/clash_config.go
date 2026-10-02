package singbox

import (
	"encoding/json"
	"fmt"
)

const panelClashController = "127.0.0.1:10090"

// MarshalJSON keeps the local Clash controller available for panel-side
// diagnostics while preserving an explicitly configured controller, secret,
// or disabled controller (empty external_controller).
func (c Config) MarshalJSON() ([]byte, error) {
	type configAlias Config
	copyConfig := configAlias(c)

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
	if controller, exists := clashAPI["external_controller"]; !exists {
		clashAPI["external_controller"] = panelClashController
	} else if _, ok := controller.(string); !ok {
		return nil, fmt.Errorf("experimental.clash_api.external_controller must be a string")
	}

	experimental["clash_api"] = clashAPI
	copyConfig.Experimental = experimental
	return json.Marshal(copyConfig)
}
