package singbox

import (
	"encoding/json"
	"fmt"
	"maps"
)

type configJSON Config

func (c *Config) MarshalJSON() ([]byte, error) {
	if c == nil {
		return []byte("null"), nil
	}

	clone := configJSON(*c)
	clone.Endpoints = make([]map[string]any, 0, len(c.Endpoints))
	seen := make(map[string]string, len(c.Outbounds)+len(c.Endpoints))

	for _, outbound := range c.Outbounds {
		tag := rawString(outbound, "tag")
		if tag == "" {
			continue
		}
		if previous, exists := seen[tag]; exists {
			return nil, fmt.Errorf("duplicate sing-box tag %q used by %s and outbound", tag, previous)
		}
		seen[tag] = "outbound"
	}

	for _, source := range c.Endpoints {
		endpoint := maps.Clone(source)
		tag := rawString(endpoint, "tag")
		if tag != "" {
			if previous, exists := seen[tag]; exists {
				return nil, fmt.Errorf("duplicate sing-box tag %q used by %s and endpoint", tag, previous)
			}
			seen[tag] = "endpoint"
		}

		if rawString(endpoint, "type") == "wireguard" {
			// Xray's noKernelTun=false is auto-detect; sing-box system=true requires privileges.
			if system, _ := endpoint["system"].(bool); system && rawString(endpoint, "name") == tag {
				delete(endpoint, "system")
				delete(endpoint, "name")
			}
			delete(endpoint, "reserved")
		}
		clone.Endpoints = append(clone.Endpoints, endpoint)
	}

	return json.Marshal(clone)
}
