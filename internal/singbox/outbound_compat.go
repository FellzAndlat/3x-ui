package singbox

import (
	"fmt"
	"maps"
	"net"
	"strconv"
	"strings"
)

func xrayBool(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

func xrayEnabled(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v > 0
	case int64:
		return v > 0
	case float64:
		return v > 0
	case string:
		if strings.EqualFold(strings.TrimSpace(v), "true") {
			return true
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return err == nil && n > 0
	default:
		return false
	}
}

func xrayUoTVersion(server map[string]any) int {
	version := rawInt(server, "uotVersion")
	if version == 0 {
		version = rawInt(server, "UoTVersion")
	}
	if version == 0 {
		version = 1
	}
	return version
}

func translateHTTPOutboundOptions(out map[string]any, settings map[string]any) {
	if headers, ok := settings["headers"].(map[string]any); ok && len(headers) > 0 {
		out["headers"] = maps.Clone(headers)
	}
}

func translateShadowsocksOutboundOptions(out map[string]any, settings map[string]any, tag string) error {
	server := firstObject(settings, "servers")
	if server == nil || !xrayBool(server, "uot") {
		return nil
	}
	version := xrayUoTVersion(server)
	if version != 1 && version != 2 {
		return fmt.Errorf("outbound %q has unsupported Shadowsocks UoT version %d", tag, version)
	}
	out["udp_over_tcp"] = map[string]any{"enabled": true, "version": version}
	return nil
}

func translateSendThrough(out map[string]any, raw map[string]any, tag string) error {
	value := strings.TrimSpace(rawString(raw, "sendThrough"))
	if value == "" || value == "0.0.0.0" || value == "::" {
		return nil
	}
	if strings.Contains(value, "/") {
		return fmt.Errorf("outbound %q uses sendThrough CIDR %q which sing-box cannot represent", tag, value)
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return fmt.Errorf("outbound %q has invalid sendThrough address %q", tag, value)
	}
	if ip.To4() != nil {
		out["inet4_bind_address"] = ip.String()
	} else {
		out["inet6_bind_address"] = ip.String()
	}
	return nil
}

func xrayDomainResolver(value, tag string) (map[string]any, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "AsIs") {
		return nil, nil
	}
	resolver := map[string]any{"server": "local"}
	switch strings.ToLower(value) {
	case "useip", "forceip":
	case "useipv4", "forceipv4":
		resolver["strategy"] = "ipv4_only"
	case "useipv6", "forceipv6":
		resolver["strategy"] = "ipv6_only"
	case "useipv4v6", "forceipv4v6":
		resolver["strategy"] = "prefer_ipv4"
	case "useipv6v4", "forceipv6v4":
		resolver["strategy"] = "prefer_ipv6"
	default:
		return nil, fmt.Errorf("outbound %q uses unknown Xray domainStrategy %q", tag, value)
	}
	return resolver, nil
}

func translateRoutingMark(out map[string]any, sockopt map[string]any, tag string) error {
	value, exists := sockopt["mark"]
	if !exists || value == nil {
		return nil
	}
	switch mark := value.(type) {
	case string:
		mark = strings.TrimSpace(mark)
		if mark == "" || mark == "0" || strings.EqualFold(mark, "0x0") {
			return nil
		}
		number := mark
		base := 10
		if strings.HasPrefix(strings.ToLower(number), "0x") {
			number = number[2:]
			base = 16
		}
		if _, err := strconv.ParseUint(number, base, 32); err != nil {
			return fmt.Errorf("outbound %q has invalid routing mark %q", tag, mark)
		}
		out["routing_mark"] = mark
	default:
		mark := rawInt(sockopt, "mark")
		if mark < 0 {
			return fmt.Errorf("outbound %q has invalid negative routing mark", tag)
		}
		if mark > 0 {
			out["routing_mark"] = mark
		}
	}
	return nil
}

func translateOutboundSockopt(out map[string]any, stream map[string]any, tag string) error {
	sockopt := rawObject(stream, "sockopt")
	if len(sockopt) == 0 {
		return nil
	}
	if bindInterface := strings.TrimSpace(rawString(sockopt, "interface")); bindInterface != "" {
		out["bind_interface"] = bindInterface
	}
	if err := translateRoutingMark(out, sockopt, tag); err != nil {
		return err
	}
	if xrayEnabled(sockopt["tcpFastOpen"]) {
		out["tcp_fast_open"] = true
	}
	if xrayEnabled(sockopt["tcpMptcp"]) {
		out["tcp_multi_path"] = true
	}
	if idle := rawInt(sockopt, "tcpKeepAliveIdle"); idle > 0 {
		out["tcp_keep_alive"] = fmt.Sprintf("%ds", idle)
	}
	if interval := rawInt(sockopt, "tcpKeepAliveInterval"); interval > 0 {
		out["tcp_keep_alive_interval"] = fmt.Sprintf("%ds", interval)
	}
	if detour := strings.TrimSpace(rawString(sockopt, "dialerProxy")); detour != "" {
		out["detour"] = detour
	}
	resolver, err := xrayDomainResolver(rawString(sockopt, "domainStrategy"), tag)
	if err != nil {
		return err
	}
	if resolver != nil {
		out["domain_resolver"] = resolver
	}
	return nil
}

func ensureRequiredOutboundTLS(out map[string]any, protocol string) {
	if protocol != "tuic" {
		return
	}
	tls, ok := out["tls"].(map[string]any)
	if !ok || tls == nil {
		tls = map[string]any{}
		out["tls"] = tls
	}
	if _, exists := tls["enabled"]; !exists {
		tls["enabled"] = true
	}
}

func validateXrayOutboundOnlyOptions(raw map[string]any, protocol, tag string) error {
	mux := rawObject(raw, "mux")
	if xrayBool(mux, "enabled") {
		return fmt.Errorf("outbound %q enables Xray Mux.Cool; sing-box multiplex is a different protocol", tag)
	}
	strategy := strings.TrimSpace(rawString(raw, "targetStrategy"))
	if protocol != "freedom" && strategy != "" && !strings.EqualFold(strategy, "AsIs") {
		return fmt.Errorf("outbound %q uses Xray targetStrategy %q which has no equivalent sing-box outbound option", tag, strategy)
	}
	return nil
}

func applyXrayOutboundCompatibility(out map[string]any, raw map[string]any, stream map[string]any) error {
	protocol := strings.ToLower(strings.TrimSpace(rawString(raw, "protocol")))
	tag := rawString(raw, "tag")
	settings := rawObject(raw, "settings")

	if err := validateXrayOutboundOnlyOptions(raw, protocol, tag); err != nil {
		return err
	}
	switch protocol {
	case "http":
		translateHTTPOutboundOptions(out, settings)
	case "shadowsocks":
		if err := translateShadowsocksOutboundOptions(out, settings, tag); err != nil {
			return err
		}
	}
	ensureRequiredOutboundTLS(out, protocol)
	if rawString(out, "type") == "block" {
		return nil
	}
	if err := translateSendThrough(out, raw, tag); err != nil {
		return err
	}
	return translateOutboundSockopt(out, stream, tag)
}
