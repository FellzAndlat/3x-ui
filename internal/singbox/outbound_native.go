package singbox

import (
	"fmt"
	"maps"
	"strings"
)

const nativeSingBoxProtocolPrefix = "singbox:"

// translateNativeSingBoxOutbound converts an Xray-shaped outbound wrapper
// whose protocol is already a sing-box outbound type. The wrapper is used by
// the panel to store outbounds for both cores, while settings contains the
// native sing-box options for protocols that do not have an Xray equivalent.
//
// An explicit "singbox:<type>" wrapper is also accepted. It is useful when a
// protocol name exists in both compatibility modes (for example TUIC or
// Hysteria2): the prefix forces native settings pass-through instead of the
// Xray-shape compatibility translator. The prefix never reaches sing-box.
//
// Only settings is flattened intentionally: Xray-only top-level fields must
// not leak into the sing-box JSON. Common dialer options are translated below
// through the compatibility helpers for outbound types that support them.
func translateNativeSingBoxOutbound(raw map[string]any, protocol, tag string) (map[string]any, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" {
		return nil, fmt.Errorf("outbound %q has an empty protocol", tag)
	}

	nativeProtocol := protocol
	if strings.HasPrefix(nativeProtocol, nativeSingBoxProtocolPrefix) {
		nativeProtocol = strings.TrimSpace(strings.TrimPrefix(nativeProtocol, nativeSingBoxProtocolPrefix))
		if nativeProtocol == "" {
			return nil, fmt.Errorf("outbound %q has an empty native sing-box protocol", tag)
		}
	}

	settings := map[string]any{}
	if value, exists := raw["settings"]; exists && value != nil {
		var ok bool
		settings, ok = value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("outbound %q protocol %s has invalid settings", tag, protocol)
		}
	}

	out := maps.Clone(settings)
	if out == nil {
		out = map[string]any{}
	}

	// Identity belongs to the panel wrapper. Do not allow imported settings to
	// spoof another outbound type or tag.
	out["type"] = nativeProtocol
	out["tag"] = tag

	if nativeOutboundSupportsDialCompatibility(nativeProtocol) {
		if err := applyXrayOutboundCompatibility(out, raw, rawObject(raw, "streamSettings")); err != nil {
			return nil, err
		}
	}
	if err := validateNativeSingBoxOutbound(out, nativeProtocol, tag); err != nil {
		return nil, err
	}
	return out, nil
}

func nativeOutboundSupportsDialCompatibility(protocol string) bool {
	// These outbounds do not expose sing-box Dial Fields. Their configuration
	// must come exclusively from native settings; translating Xray sendThrough
	// or sockopt here would emit fields rejected by sing-box.
	switch protocol {
	case "bridge", "selector", "urltest":
		return false
	default:
		return true
	}
}

func nativeOutboundGroupTags(value any, ownerTag, protocol string) ([]string, error) {
	var values []any
	switch typed := value.(type) {
	case []string:
		values = make([]any, len(typed))
		for i, item := range typed {
			values[i] = item
		}
	case []any:
		values = typed
	default:
		return nil, fmt.Errorf("sing-box outbound %q %s requires an outbounds array", ownerTag, protocol)
	}

	if len(values) == 0 {
		return nil, fmt.Errorf("sing-box outbound %q %s requires at least one outbound tag", ownerTag, protocol)
	}

	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		member, ok := value.(string)
		if !ok || strings.TrimSpace(member) == "" || member != strings.TrimSpace(member) {
			return nil, fmt.Errorf("sing-box outbound %q %s has invalid outbound tag at index %d", ownerTag, protocol, index)
		}
		if member == ownerTag {
			return nil, fmt.Errorf("sing-box outbound %q %s cannot reference itself", ownerTag, protocol)
		}
		if _, exists := seen[member]; exists {
			return nil, fmt.Errorf("sing-box outbound %q %s contains duplicate outbound tag %q", ownerTag, protocol, member)
		}
		seen[member] = struct{}{}
		out = append(out, member)
	}
	return out, nil
}

func validateNativeSingBoxOutbound(out map[string]any, protocol, tag string) error {
	if isKnownSingBoxEndpoint(protocol) {
		return fmt.Errorf("outbound %q uses sing-box endpoint type %q; configure it as an endpoint instead", tag, protocol)
	}

	// Reuse the same known-protocol validation that is applied during final
	// config marshaling. Unknown future protocols intentionally remain
	// pass-through compatible and are validated by the installed sing-box.
	return validateSingBoxOutbound(out)
}

func validateNativeOutboundGroup(out map[string]any, protocol, tag string) error {
	// Group outbounds require a non-empty list of valid member tags. Validate
	// the shape here so malformed panel JSON fails before sing-box is started.
	if protocol == "selector" || protocol == "urltest" {
		members, err := nativeOutboundGroupTags(out["outbounds"], tag, protocol)
		if err != nil {
			return err
		}
		if protocol == "selector" {
			if value, exists := out["default"]; exists {
				defaultTag, ok := value.(string)
				if !ok || defaultTag != strings.TrimSpace(defaultTag) {
					return fmt.Errorf("sing-box outbound %q selector has invalid default tag", tag)
				}
				if defaultTag == "" {
					return nil
				}
				found := false
				for _, member := range members {
					if member == defaultTag {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("sing-box outbound %q selector default %q is not present in outbounds", tag, defaultTag)
				}
			}
		}
	}

	return nil
}
