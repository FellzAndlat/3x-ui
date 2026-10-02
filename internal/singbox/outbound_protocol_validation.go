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
