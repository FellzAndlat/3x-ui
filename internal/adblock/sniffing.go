package adblock

import "encoding/json"

// XraySniffing recovers HTTP Host/TLS SNI/QUIC domains for IP-addressed
// connections, without changing stored inbound settings. Existing destination
// override choices are preserved; newly enabled sniffing is routing-only.
func XraySniffing(raw []byte) ([]byte, error) {
	sniff, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	var enabled bool
	_ = json.Unmarshal(sniff["enabled"], &enabled)
	if !enabled {
		sniff["routeOnly"] = json.RawMessage(`true`)
	}
	sniff["enabled"] = json.RawMessage(`true`)
	sniff["metadataOnly"] = json.RawMessage(`false`)
	var overrides []string
	if value := sniff["destOverride"]; len(value) > 0 {
		if err := json.Unmarshal(value, &overrides); err != nil {
			return nil, err
		}
	}
	for _, protocol := range []string{"http", "tls", "quic"} {
		found := false
		for _, value := range overrides {
			if value == protocol {
				found = true
				break
			}
		}
		if !found {
			overrides = append(overrides, protocol)
		}
	}
	sniff["destOverride"], _ = json.Marshal(overrides)
	return json.Marshal(sniff)
}
