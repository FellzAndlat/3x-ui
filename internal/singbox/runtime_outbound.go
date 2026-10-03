package singbox

import (
	"encoding/json"
	"fmt"
	"strings"
)

var knownV2RayTransportFields = []string{
	"host",
	"path",
	"method",
	"headers",
	"idle_timeout",
	"ping_timeout",
	"max_early_data",
	"early_data_header_name",
	"service_name",
	"permit_without_stream",
}

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
	if err := validateRuntimeOutboundDetours(normalized); err != nil {
		return nil, err
	}

	for index, outbound := range normalized {
		outboundType := strings.ToLower(strings.TrimSpace(rawString(outbound, "type")))
		switch outboundType {
		case "tun", "redirect", "tproxy":
			return nil, fmt.Errorf("outbounds[%d].type %q is an inbound type and cannot be used as a sing-box outbound", index, outboundType)
		}
		if err := validateRuntimeHysteriaOutbound(outbound, outboundType); err != nil {
			return nil, fmt.Errorf("outbounds[%d]: %w", index, err)
		}

		transport, ok := outbound["transport"].(map[string]any)
		if !ok || transport == nil {
			continue
		}
		transportType := strings.ToLower(strings.TrimSpace(rawString(transport, "type")))
		if err := normalizeV2RayTransportForRuntime(transport, transportType); err != nil {
			return nil, fmt.Errorf("outbounds[%d].transport: %w", index, err)
		}
	}
	return normalized, nil
}

func validateRuntimeOutboundDetours(outbounds []map[string]any) error {
	knownTags := make(map[string]struct{}, len(outbounds))
	for _, outbound := range outbounds {
		if tag := strings.TrimSpace(rawString(outbound, "tag")); tag != "" {
			knownTags[tag] = struct{}{}
		}
	}

	edges := make(map[string]string, len(outbounds))
	for _, outbound := range outbounds {
		tag := strings.TrimSpace(rawString(outbound, "tag"))
		detour := strings.TrimSpace(rawString(outbound, "detour"))
		if tag == "" || detour == "" {
			continue
		}
		if detour == tag {
			return fmt.Errorf("sing-box outbound %q cannot detour to itself", tag)
		}
		// Endpoint tags can also act as outbound targets. Only add an edge when
		// both ends are in the outbound list; endpoint references are validated
		// by sing-box after the full config is assembled.
		if _, exists := knownTags[detour]; exists {
			edges[tag] = detour
		}
	}

	state := make(map[string]uint8, len(edges))
	stack := make([]string, 0, len(edges))
	var visit func(string) error
	visit = func(tag string) error {
		switch state[tag] {
		case 1:
			start := 0
			for i, item := range stack {
				if item == tag {
					start = i
					break
				}
			}
			cycle := append(append([]string(nil), stack[start:]...), tag)
			return fmt.Errorf("sing-box outbound detour cycle: %s", strings.Join(cycle, " -> "))
		case 2:
			return nil
		}

		state[tag] = 1
		stack = append(stack, tag)
		if next, exists := edges[tag]; exists {
			if err := visit(next); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[tag] = 2
		return nil
	}

	for tag := range edges {
		if err := visit(tag); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeHysteriaOutbound(outbound map[string]any, outboundType string) error {
	if outboundType != "hysteria" && outboundType != "hysteria2" {
		return nil
	}
	if stringSliceLength(outbound["server_ports"]) > 0 && rawInt(outbound, "server_port") != 0 {
		return fmt.Errorf("%s cannot combine server_port with server_ports", outboundType)
	}
	if outboundType != "hysteria2" {
		return nil
	}

	realm := rawObject(outbound, "realm")
	if len(realm) == 0 {
		return nil
	}
	ipVersion := rawInt(realm, "ip_version")
	if ipVersion != 0 && ipVersion != 4 && ipVersion != 6 {
		return fmt.Errorf("hysteria2 realm.ip_version must be 4 or 6 when set")
	}
	if ipVersion == 6 && hysteria2RealmPortMappingConfigured(rawObject(realm, "port_mapping")) {
		return fmt.Errorf("hysteria2 realm.port_mapping requires IPv4 and cannot be used with ip_version 6")
	}
	return nil
}

func hysteria2RealmPortMappingConfigured(portMapping map[string]any) bool {
	if len(portMapping) == 0 {
		return false
	}
	if enabled, ok := portMapping["enabled"].(bool); ok && enabled {
		return true
	}
	return strings.TrimSpace(rawString(portMapping, "timeout")) != "" || strings.TrimSpace(rawString(portMapping, "lifetime")) != ""
}

func normalizeV2RayTransportForRuntime(transport map[string]any, transportType string) error {
	var allowed map[string]struct{}
	switch transportType {
	case "http":
		allowed = stringSet("host", "path", "method", "headers", "idle_timeout", "ping_timeout")
		if err := normalizeHTTPTransportHost(transport); err != nil {
			return err
		}
	case "ws":
		allowed = stringSet("path", "headers", "max_early_data", "early_data_header_name")
	case "quic":
		// Current sing-box QUIC transport has no protocol-specific options.
		allowed = map[string]struct{}{}
	case "grpc":
		allowed = stringSet("service_name", "idle_timeout", "ping_timeout", "permit_without_stream")
	case "httpupgrade":
		allowed = stringSet("host", "path", "headers")
	default:
		// Let sing-box validate unknown/new transport types. Do not destroy
		// fields that this panel version does not understand yet.
		return nil
	}

	// The editor keeps the previous transport object when its type changes.
	// Strip only fields known to belong to another current V2Ray transport;
	// unrelated/unknown keys are left for sing-box to validate so future schema
	// additions are not silently discarded by an older panel.
	for _, key := range knownV2RayTransportFields {
		if _, ok := allowed[key]; !ok {
			delete(transport, key)
		}
	}
	return nil
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
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
