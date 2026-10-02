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

func translateFlatProxyCredentials(out map[string]any, settings map[string]any) {
	if username := compatStringOption(settings, "user", "username"); username != "" {
		out["username"] = username
	}
	if password := compatStringOption(settings, "pass", "password"); password != "" {
		out["password"] = password
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

func compatStringOption(settings map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(rawString(settings, key)); value != "" {
			return value
		}
	}
	return ""
}

func compatBoolOption(settings map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if value, ok := settings[key].(bool); ok {
			return value, true
		}
	}
	return false, false
}

func translateTUICOutboundOptions(out map[string]any, settings map[string]any) {
	if value := compatStringOption(settings, "congestion_control", "congestionControl"); value != "" {
		out["congestion_control"] = strings.ToLower(value)
	}
	if value := compatStringOption(settings, "udp_relay_mode", "udpRelayMode"); value != "" {
		out["udp_relay_mode"] = strings.ToLower(value)
	}
	if value, ok := compatBoolOption(settings, "udp_over_stream", "udpOverStream"); ok {
		out["udp_over_stream"] = value
	}
	if value, ok := compatBoolOption(settings, "zero_rtt_handshake", "zeroRttHandshake"); ok {
		out["zero_rtt_handshake"] = value
	}
	if value := compatStringOption(settings, "heartbeat"); value != "" {
		out["heartbeat"] = value
	}
	if value := compatStringOption(settings, "network"); value != "" {
		out["network"] = strings.ToLower(value)
	}
}

func xrayV2RayUser(settings map[string]any) map[string]any {
	server := firstObject(settings, "vnext")
	if server == nil {
		return nil
	}
	users, _ := server["users"].([]any)
	if len(users) == 0 {
		return nil
	}
	user, _ := users[0].(map[string]any)
	return user
}

func xrayPacketEncoding(settings map[string]any) string {
	if value := compatStringOption(settings, "packetEncoding", "packet_encoding"); value != "" {
		return value
	}
	if user := xrayV2RayUser(settings); user != nil {
		return compatStringOption(user, "packetEncoding", "packet_encoding")
	}
	return ""
}

func translateV2RayPacketEncoding(out map[string]any, settings map[string]any, protocol, tag string) error {
	if protocol != "vless" && protocol != "vmess" {
		return nil
	}

	specialVisionUDP443 := false
	if protocol == "vless" {
		flow := strings.ToLower(strings.TrimSpace(rawString(out, "flow")))
		switch flow {
		case "":
		case "xtls-rprx-vision":
			out["flow"] = "xtls-rprx-vision"
		case "xtls-rprx-vision-udp443":
			out["flow"] = "xtls-rprx-vision"
			out["packet_encoding"] = "xudp"
			specialVisionUDP443 = true
		default:
			return fmt.Errorf("outbound %q VLESS has unsupported flow %q", tag, rawString(out, "flow"))
		}
	}

	value := strings.ToLower(strings.TrimSpace(xrayPacketEncoding(settings)))
	switch value {
	case "":
		return nil
	case "none":
		if !specialVisionUDP443 {
			delete(out, "packet_encoding")
		}
		return nil
	case "xudp":
		out["packet_encoding"] = "xudp"
		return nil
	case "packetaddr":
		if specialVisionUDP443 {
			return fmt.Errorf("outbound %q VLESS flow xtls-rprx-vision-udp443 conflicts with packetEncoding %q", tag, value)
		}
		out["packet_encoding"] = "packetaddr"
		return nil
	default:
		return fmt.Errorf("outbound %q %s has unsupported packetEncoding %q", tag, strings.ToUpper(protocol), value)
	}
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
		if strings.HasPrefix(strings.ToLower(mark), "0x") {
			number := mark[2:]
			if _, err := strconv.ParseUint(number, 16, 32); err != nil {
				return fmt.Errorf("outbound %q has invalid routing mark %q", tag, mark)
			}
			out["routing_mark"] = mark
			return nil
		}
		parsed, err := strconv.ParseUint(mark, 10, 32)
		if err != nil {
			return fmt.Errorf("outbound %q has invalid routing mark %q", tag, mark)
		}
		out["routing_mark"] = int(parsed)
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
	idle := rawInt(sockopt, "tcpKeepAliveIdle")
	interval := rawInt(sockopt, "tcpKeepAliveInterval")
	if idle < 0 || interval < 0 {
		out["disable_tcp_keep_alive"] = true
	} else {
		if idle > 0 {
			out["tcp_keep_alive"] = fmt.Sprintf("%ds", idle)
		}
		if interval > 0 {
			out["tcp_keep_alive_interval"] = fmt.Sprintf("%ds", interval)
		}
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

func ensureOutboundDomainResolver(out map[string]any) {
	if _, exists := out["domain_resolver"]; exists {
		return
	}
	if strings.TrimSpace(rawString(out, "detour")) != "" {
		return
	}
	server := strings.TrimSpace(rawString(out, "server"))
	if server == "" {
		return
	}
	if ip := net.ParseIP(strings.Trim(server, "[]")); ip != nil {
		return
	}
	out["domain_resolver"] = "local"
}

func wireGuardPeerUsesDomain(peer map[string]any) bool {
	address := strings.TrimSpace(rawString(peer, "address"))
	if address == "" {
		return false
	}
	return net.ParseIP(strings.Trim(address, "[]")) == nil
}

func ensureWireGuardEndpointDomainResolver(endpoint map[string]any) {
	if _, exists := endpoint["domain_resolver"]; exists {
		return
	}
	if strings.TrimSpace(rawString(endpoint, "detour")) != "" {
		return
	}
	switch peers := endpoint["peers"].(type) {
	case []map[string]any:
		for _, peer := range peers {
			if wireGuardPeerUsesDomain(peer) {
				endpoint["domain_resolver"] = "local"
				return
			}
		}
	case []any:
		for _, item := range peers {
			peer, ok := item.(map[string]any)
			if ok && wireGuardPeerUsesDomain(peer) {
				endpoint["domain_resolver"] = "local"
				return
			}
		}
	}
}

func applyXrayWireGuardEndpointCompatibility(endpoint map[string]any, raw map[string]any) error {
	tag := rawString(raw, "tag")
	if err := validateXrayOutboundOnlyOptions(raw, "wireguard", tag); err != nil {
		return err
	}
	if err := translateSendThrough(endpoint, raw, tag); err != nil {
		return err
	}
	if err := translateOutboundSockopt(endpoint, rawObject(raw, "streamSettings"), tag); err != nil {
		return err
	}
	ensureWireGuardEndpointDomainResolver(endpoint)
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
	stream := rawObject(raw, "streamSettings")
	if strings.EqualFold(strings.TrimSpace(rawString(stream, "network")), "quic") {
		return fmt.Errorf("outbound %q uses Xray QUIC transport, which is not wire-compatible with sing-box V2Ray QUIC", tag)
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
	case "socks":
		translateFlatProxyCredentials(out, settings)
	case "http":
		translateFlatProxyCredentials(out, settings)
		translateHTTPOutboundOptions(out, settings)
	case "shadowsocks":
		if err := translateShadowsocksOutboundOptions(out, settings, tag); err != nil {
			return err
		}
	case "vmess", "vless":
		if err := translateV2RayPacketEncoding(out, settings, protocol, tag); err != nil {
			return err
		}
	case "tuic":
		translateTUICOutboundOptions(out, settings)
	}
	ensureRequiredOutboundTLS(out, protocol)
	if rawString(out, "type") == "block" {
		return nil
	}
	if err := translateSendThrough(out, raw, tag); err != nil {
		return err
	}
	if err := translateOutboundSockopt(out, stream, tag); err != nil {
		return err
	}
	ensureOutboundDomainResolver(out)
	return nil
}
