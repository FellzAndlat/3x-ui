package singbox

import (
	"encoding/json"
	"fmt"
	"strings"
)

// normalizeOutboundsForRuntime converts panel-generated conveniences to the
// strict sing-box outbound schema without mutating the stored editor template.
func normalizeOutboundsForRuntime(outbounds []map[string]any) ([]map[string]any, error) {
	if outbounds == nil {
		return nil, nil
	}
	data, err := json.Marshal(outbounds)
	if err != nil {
		return nil, fmt.Errorf("clone outbound config: %w", err)
	}
	var normalized []map[string]any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, fmt.Errorf("clone outbound config: %w", err)
	}

	for index, outbound := range normalized {
		outboundType := strings.ToLower(strings.TrimSpace(rawString(outbound, "type")))
		switch outboundType {
		case "tun", "redirect", "tproxy":
			return nil, fmt.Errorf("outbounds[%d].type %q is an inbound type and cannot be used as a sing-box outbound", index, outboundType)
		}

		transport, ok := outbound["transport"].(map[string]any)
		if !ok || transport == nil {
			continue
		}
		transportType := strings.ToLower(strings.TrimSpace(rawString(transport, "type")))
		switch transportType {
		case "http":
			if err := normalizeHTTPTransportHost(transport); err != nil {
				return nil, fmt.Errorf("outbounds[%d].transport: %w", index, err)
			}
		case "quic":
			// Current sing-box QUIC transport has no protocol-specific options.
			// The editor intentionally keeps the previous transport object when a
			// user changes its type, so switching HTTP/WS/gRPC -> QUIC can leave
			// stale path/headers/host/service_name fields behind. Strict sing-box
			// decoding rejects those fields. Preserve only the discriminator in
			// the runtime copy while leaving the stored editor template untouched.
			for key := range transport {
				if key != "type" {
					delete(transport, key)
				}
			}
		}
	}
	return normalized, nil
}

func normalizeHTTPTransportHost(transport map[string]any) error {
	if _, exists := transport["host"]; exists {
		return nil
	}
	headers, ok := transport["headers"].(map[string]any)
	if !ok || headers == nil {
		return nil
	}

	var hostKey string
	var hostValue any
	for key, value := range headers {
		if strings.EqualFold(key, "host") {
			hostKey = key
			hostValue = value
			break
		}
	}
	if hostKey == "" {
		return nil
	}

	hosts, err := runtimeHostList(hostValue)
	if err != nil {
		return err
	}
	if len(hosts) > 0 {
		transport["host"] = hosts
	}
	delete(headers, hostKey)
	if len(headers) == 0 {
		delete(transport, "headers")
	}
	return nil
}

func runtimeHostList(value any) ([]string, error) {
	switch host := value.(type) {
	case string:
		host = strings.TrimSpace(host)
		if host == "" {
			return nil, nil
		}
		return []string{host}, nil
	case []string:
		result := make([]string, 0, len(host))
		for _, item := range host {
			item = strings.TrimSpace(item)
			if item != "" {
				result = append(result, item)
			}
		}
		return result, nil
	case []any:
		result := make([]string, 0, len(host))
		for _, raw := range host {
			item, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("HTTP transport Host header must be a string or string array")
			}
			item = strings.TrimSpace(item)
			if item != "" {
				result = append(result, item)
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("HTTP transport Host header must be a string or string array")
	}
}
