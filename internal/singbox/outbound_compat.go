package singbox

import (
	"fmt"
	"maps"
	"net"
	"strings"
)

func xrayBool(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
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

func translateSockoptDomainResolver(out map[string]any, sockopt map[string]any) {
	strategy := TranslateXrayDomainStrategy(rawString(sockopt, "domainStrategy"))
	if strategy == "" {
		return
	}
	out["domain_resolver"] = map[string]any{"server": "local", "strategy": strategy}
}

func translateOutboundSockopt(out map[string]any, stream map[string]any) {
	sockopt := rawObject(stream, "sockopt")
	if len(sockopt) == 0 {
		return
	}
	if bindInterface := strings.TrimSpace(rawString(sockopt, "interface")); bindInterface != "" {
		out["bind_interface"] = bindInterface
	}
	if mark := rawInt(sockopt, "mark"); mark > 0 {
		out["routing_mark"] = mark
	}
	if xrayBool(sockopt, "tcpFastOpen") {
		out["tcp_fast_open"] = true
	}
	if xrayBool(sockopt, "tcpMptcp") {
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
	translateSockoptDomainResolver(out, sockopt)
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
	if rawString(out, "type") == "block" {
		return nil
	}
	if err := translateSendThrough(out, raw, tag); err != nil {
		return err
	}
	translateOutboundSockopt(out, stream)
	return nil
}
