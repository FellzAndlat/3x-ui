package singbox

import (
	"encoding/json"
	"fmt"
	"maps"
)

type configJSON Config

func singBoxOutboundSupportsTLS(protocol string) bool {
	switch protocol {
	case "http", "vmess", "vless", "trojan", "hysteria", "hysteria2", "tuic":
		return true
	default:
		return false
	}
}

func singBoxOutboundSupportsReality(protocol string) bool {
	switch protocol {
	case "http", "vmess", "vless", "trojan":
		return true
	default:
		return false
	}
}

func singBoxOutboundSupportsV2RayTransport(protocol string) bool {
	switch protocol {
	case "vmess", "vless", "trojan":
		return true
	default:
		return false
	}
}

func validateSingBoxOutbound(outbound map[string]any) error {
	protocol := rawString(outbound, "type")
	tag := rawString(outbound, "tag")

	if tls, ok := outbound["tls"].(map[string]any); ok && len(tls) > 0 {
		if !singBoxOutboundSupportsTLS(protocol) {
			return fmt.Errorf("sing-box outbound %q protocol %s does not support TLS settings", tag, protocol)
		}
		if reality, ok := tls["reality"].(map[string]any); ok && len(reality) > 0 && !singBoxOutboundSupportsReality(protocol) {
			return fmt.Errorf("sing-box outbound %q protocol %s does not support REALITY", tag, protocol)
		}
	} else if protocol == "tuic" {
		return fmt.Errorf("sing-box outbound %q TUIC requires TLS", tag)
	}

	transport, ok := outbound["transport"].(map[string]any)
	if !ok || len(transport) == 0 {
		return nil
	}
	if !singBoxOutboundSupportsV2RayTransport(protocol) {
		return fmt.Errorf("sing-box outbound %q protocol %s does not support V2Ray transport", tag, protocol)
	}
	switch rawString(transport, "type") {
	case "http", "ws", "quic", "grpc", "httpupgrade":
		return nil
	default:
		return fmt.Errorf("sing-box outbound %q uses unsupported V2Ray transport %q", tag, rawString(transport, "type"))
	}
}

func (c *Config) MarshalJSON() ([]byte, error) {
	if c == nil {
		return []byte("null"), nil
	}

	clone := configJSON(*c)
	clone.Outbounds = make([]map[string]any, 0, len(c.Outbounds))
	clone.Endpoints = make([]map[string]any, 0, len(c.Endpoints))
	seen := make(map[string]string, len(c.Outbounds)+len(c.Endpoints))

	for _, source := range c.Outbounds {
		outbound := maps.Clone(source)
		if err := validateSingBoxOutbound(outbound); err != nil {
			return nil, err
		}
		tag := rawString(outbound, "tag")
		if tag != "" {
			if previous, exists := seen[tag]; exists {
				return nil, fmt.Errorf("duplicate sing-box tag %q used by %s and outbound", tag, previous)
			}
			seen[tag] = "outbound"
		}
		clone.Outbounds = append(clone.Outbounds, outbound)
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
			delete(endpoint, "reserved")
		}
		clone.Endpoints = append(clone.Endpoints, endpoint)
	}

	return json.Marshal(clone)
}
