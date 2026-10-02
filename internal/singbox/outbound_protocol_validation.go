package singbox

import (
	"fmt"
	"strings"
)

// finalizeTranslatedOutbound applies protocol requirements that are shared by
// legacy and modern Xray panel shapes after the protocol-specific translation
// and stream conversion have completed.
func finalizeTranslatedOutbound(out map[string]any, protocol string, settings, streamSettings map[string]any) error {
	tag := rawString(out, "tag")
	if port := rawInt(out, "server_port"); port > 65535 {
		return fmt.Errorf("outbound %q has an invalid server port", tag)
	}

	switch protocol {
	case "freedom":
		if err := validateFreedomTranslation(tag, settings, streamSettings); err != nil {
			return err
		}
	case "blackhole":
		if err := validateBlackholeTranslation(tag, settings); err != nil {
			return err
		}
	case "shadowsocks":
		method := strings.TrimSpace(rawString(out, "method"))
		if method == "" {
			return fmt.Errorf("outbound %q has an empty Shadowsocks method", tag)
		}
		if rawString(out, "password") == "" {
			return fmt.Errorf("outbound %q has an empty Shadowsocks password", tag)
		}

		server := firstObject(settings, "servers")
		if server != nil && rawBool(server, "uot") {
			version := rawInt(server, "UoTVersion")
			if version == 0 {
				// The existing panel form historically defaults UoTVersion to 1.
				version = 1
			}
			if version != 1 && version != 2 {
				return fmt.Errorf("outbound %q has invalid Shadowsocks UoTVersion %d", tag, version)
			}
			out["udp_over_tcp"] = map[string]any{
				"enabled": true,
				"version": version,
			}
		}

	case "tuic":
		if strings.TrimSpace(rawString(out, "uuid")) == "" {
			return fmt.Errorf("outbound %q has an empty TUIC UUID", tag)
		}
		security := strings.ToLower(strings.TrimSpace(rawString(streamSettings, "security")))
		switch security {
		case "":
			// Legacy/flat panel TUIC outbounds may omit the explicit stream
			// security marker even though TUIC always requires TLS.
			if _, exists := out["tls"]; !exists {
				out["tls"] = map[string]any{"enabled": true}
			}
		case "tls":
			if _, exists := out["tls"]; !exists {
				return fmt.Errorf("outbound %q TUIC TLS settings were not translated", tag)
			}
		default:
			return fmt.Errorf("outbound %q uses TUIC with unsupported security %q", tag, security)
		}
	}
	return nil
}

func validateFreedomTranslation(tag string, settings, streamSettings map[string]any) error {
	if value := strings.TrimSpace(rawString(settings, "redirect")); value != "" {
		return fmt.Errorf("outbound %q Freedom redirect %q cannot be represented safely by sing-box direct", tag, value)
	}
	if rawInt(settings, "userLevel") != 0 {
		return fmt.Errorf("outbound %q Freedom userLevel is not supported by sing-box direct", tag)
	}
	if rawInt(settings, "proxyProtocol") != 0 {
		return fmt.Errorf("outbound %q Freedom proxyProtocol is not supported by sing-box direct", tag)
	}
	if fragment := rawObject(settings, "fragment"); len(fragment) > 0 {
		return fmt.Errorf("outbound %q Freedom fragment cannot be represented by sing-box direct", tag)
	}
	if values, ok := settings["noises"].([]any); ok && len(values) > 0 {
		return fmt.Errorf("outbound %q Freedom noises cannot be represented by sing-box direct", tag)
	}
	if values, ok := settings["finalRules"].([]any); ok && len(values) > 0 {
		return fmt.Errorf("outbound %q Freedom finalRules cannot be represented by sing-box direct", tag)
	}

	for _, source := range []map[string]any{settings, rawObject(streamSettings, "sockopt")} {
		strategy := strings.TrimSpace(rawString(source, "domainStrategy"))
		if strategy != "" && !strings.EqualFold(strategy, "AsIs") {
			return fmt.Errorf("outbound %q Freedom domainStrategy %q is not translated to sing-box direct", tag, strategy)
		}
	}
	return nil
}

func validateBlackholeTranslation(tag string, settings map[string]any) error {
	response := rawObject(settings, "response")
	responseType := strings.ToLower(strings.TrimSpace(rawString(response, "type")))
	switch responseType {
	case "", "none":
		return nil
	case "http", "custom":
		return fmt.Errorf("outbound %q Blackhole response type %q cannot be represented by sing-box block", tag, responseType)
	default:
		return fmt.Errorf("outbound %q has unsupported Blackhole response type %q", tag, responseType)
	}
}
