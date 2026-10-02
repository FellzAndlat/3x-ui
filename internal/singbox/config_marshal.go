package singbox

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
)

type configJSON Config

func validateSingBoxOutboundType(protocol, tag string) error {
	switch protocol {
	case "":
		return fmt.Errorf("sing-box outbound %q has an empty type", tag)
	case "dns":
		return fmt.Errorf("sing-box outbound %q uses removed DNS outbound; sing-box 1.13+ requires DNS rule actions instead", tag)
	case "tun", "redirect", "tproxy":
		return fmt.Errorf("sing-box outbound %q uses %q, which is an inbound type, not an outbound", tag, protocol)
	case "wireguard":
		return fmt.Errorf("sing-box outbound %q uses legacy WireGuard outbound; sing-box 1.13+ requires a WireGuard endpoint", tag)
	default:
		// Keep unknown future outbound types pass-through compatible. The installed
		// sing-box binary remains the source of truth for types introduced after
		// the panel version, while known removed/invalid types are rejected above.
		return nil
	}
}

func isKnownSingBoxOutbound(protocol string) bool {
	switch protocol {
	case "direct", "bridge", "block", "socks", "http", "shadowsocks", "vmess", "trojan", "wireguard", "hysteria", "vless", "shadowtls", "tuic", "hysteria2", "anytls", "snell", "tailcat", "tor", "ssh", "dns", "selector", "urltest", "naive":
		return true
	default:
		return false
	}
}

func singBoxOutboundSupportsTLS(protocol string) bool {
	switch protocol {
	case "http", "vmess", "vless", "trojan", "hysteria", "hysteria2", "tuic", "shadowtls", "anytls", "naive":
		return true
	default:
		return false
	}
}

func singBoxOutboundRequiresTLS(protocol string) bool {
	switch protocol {
	case "hysteria", "hysteria2", "tuic", "shadowtls", "anytls", "naive":
		return true
	default:
		return false
	}
}

func singBoxOutboundSupportsReality(protocol string) bool {
	switch protocol {
	case "http", "vmess", "vless", "trojan", "anytls":
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

func validateSingBoxServer(outbound map[string]any, protocol, tag string) error {
	if rawString(outbound, "server") == "" {
		return fmt.Errorf("sing-box outbound %q protocol %s requires a server", tag, protocol)
	}
	if protocol == "hysteria2" {
		if ports, ok := outbound["server_ports"].([]string); ok && len(ports) > 0 {
			return nil
		}
		if ports, ok := outbound["server_ports"].([]any); ok && len(ports) > 0 {
			return nil
		}
	}
	port := rawInt(outbound, "server_port")
	if protocol == "ssh" && port == 0 {
		// sing-box defaults SSH to port 22.
		return nil
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("sing-box outbound %q protocol %s has an invalid server port", tag, protocol)
	}
	return nil
}

func validateTUICOutboundOptions(outbound map[string]any, tag string) error {
	if value := strings.ToLower(strings.TrimSpace(rawString(outbound, "congestion_control"))); value != "" {
		switch value {
		case "cubic", "new_reno", "bbr":
		default:
			return fmt.Errorf("sing-box outbound %q TUIC has invalid congestion_control %q", tag, value)
		}
	}
	mode := strings.ToLower(strings.TrimSpace(rawString(outbound, "udp_relay_mode")))
	if mode != "" {
		switch mode {
		case "native", "quic":
		default:
			return fmt.Errorf("sing-box outbound %q TUIC has invalid udp_relay_mode %q", tag, mode)
		}
	}
	if enabled, _ := outbound["udp_over_stream"].(bool); enabled && mode != "" {
		return fmt.Errorf("sing-box outbound %q TUIC cannot combine udp_over_stream with udp_relay_mode", tag)
	}
	if network := strings.ToLower(strings.TrimSpace(rawString(outbound, "network"))); network != "" && network != "tcp" && network != "udp" {
		return fmt.Errorf("sing-box outbound %q TUIC has invalid network %q", tag, network)
	}
	return nil
}

func validateNaiveOutboundTLS(outbound map[string]any, tag string) error {
	tls := rawObject(outbound, "tls")
	if insecure, _ := tls["insecure"].(bool); insecure {
		return fmt.Errorf("sing-box outbound %q Naive does not support tls.insecure", tag)
	}
	if stringSliceLength(tls["alpn"]) > 0 {
		return fmt.Errorf("sing-box outbound %q Naive does not support custom TLS ALPN", tag)
	}
	for _, key := range []string{"min_version", "max_version"} {
		if rawString(tls, key) != "" {
			return fmt.Errorf("sing-box outbound %q Naive does not support tls.%s", tag, key)
		}
	}
	for _, key := range []string{"cipher_suites", "curve_preferences", "client_certificate", "client_key"} {
		if stringSliceLength(tls[key]) > 0 {
			return fmt.Errorf("sing-box outbound %q Naive does not support tls.%s", tag, key)
		}
	}
	for _, key := range []string{"client_certificate_path", "client_key_path"} {
		if rawString(tls, key) != "" {
			return fmt.Errorf("sing-box outbound %q Naive does not support tls.%s", tag, key)
		}
	}
	for _, key := range []string{"disable_sni", "fragment", "record_fragment", "kernel_tx", "kernel_rx"} {
		if enabled, _ := tls[key].(bool); enabled {
			return fmt.Errorf("sing-box outbound %q Naive does not support tls.%s", tag, key)
		}
	}
	if utls := rawObject(tls, "utls"); xrayBool(utls, "enabled") {
		return fmt.Errorf("sing-box outbound %q Naive does not support uTLS", tag)
	}
	if reality := rawObject(tls, "reality"); xrayBool(reality, "enabled") {
		return fmt.Errorf("sing-box outbound %q Naive does not support REALITY", tag)
	}
	return nil
}

func validateSingBoxRequiredFields(outbound map[string]any, protocol, tag string) error {
	switch protocol {
	case "socks", "http", "shadowsocks", "vmess", "vless", "trojan", "hysteria", "hysteria2", "tuic", "shadowtls", "anytls", "naive", "snell", "ssh":
		if err := validateSingBoxServer(outbound, protocol, tag); err != nil {
			return err
		}
	}

	switch protocol {
	case "shadowsocks":
		if rawString(outbound, "method") == "" || rawString(outbound, "password") == "" {
			return fmt.Errorf("sing-box outbound %q Shadowsocks requires method and password", tag)
		}
	case "vmess", "vless":
		if rawString(outbound, "uuid") == "" {
			return fmt.Errorf("sing-box outbound %q %s requires UUID", tag, protocol)
		}
	case "trojan":
		if rawString(outbound, "password") == "" {
			return fmt.Errorf("sing-box outbound %q Trojan requires password", tag)
		}
	case "hysteria":
		if strings.TrimSpace(rawString(outbound, "up")) == "" && rawInt(outbound, "up_mbps") <= 0 {
			return fmt.Errorf("sing-box outbound %q Hysteria requires up or positive up_mbps", tag)
		}
		if strings.TrimSpace(rawString(outbound, "down")) == "" && rawInt(outbound, "down_mbps") <= 0 {
			return fmt.Errorf("sing-box outbound %q Hysteria requires down or positive down_mbps", tag)
		}
	case "tuic":
		if rawString(outbound, "uuid") == "" {
			return fmt.Errorf("sing-box outbound %q TUIC requires UUID", tag)
		}
		if err := validateTUICOutboundOptions(outbound, tag); err != nil {
			return err
		}
	case "shadowtls":
		if version := rawInt(outbound, "version"); version != 0 && (version < 1 || version > 3) {
			return fmt.Errorf("sing-box outbound %q ShadowTLS has unsupported version %d", tag, version)
		}
	case "anytls":
		if rawString(outbound, "password") == "" {
			return fmt.Errorf("sing-box outbound %q AnyTLS requires password", tag)
		}
	case "naive":
		if err := validateNaiveOutboundTLS(outbound, tag); err != nil {
			return err
		}
	case "snell":
		version := rawInt(outbound, "version")
		if version != 4 && version != 6 {
			return fmt.Errorf("sing-box outbound %q Snell requires version 4 or 6", tag)
		}
		if rawString(outbound, "psk") == "" {
			return fmt.Errorf("sing-box outbound %q Snell requires PSK", tag)
		}
	}
	return nil
}

func validateSingBoxOutbound(outbound map[string]any) error {
	protocol := strings.ToLower(strings.TrimSpace(rawString(outbound, "type")))
	tag := rawString(outbound, "tag")
	if err := validateSingBoxOutboundType(protocol, tag); err != nil {
		return err
	}
	if err := validateSingBoxRequiredFields(outbound, protocol, tag); err != nil {
		return err
	}

	knownProtocol := isKnownSingBoxOutbound(protocol)
	tls, hasTLS := outbound["tls"].(map[string]any)
	if knownProtocol && hasTLS && len(tls) > 0 {
		if !singBoxOutboundSupportsTLS(protocol) {
			return fmt.Errorf("sing-box outbound %q protocol %s does not support TLS settings", tag, protocol)
		}
		if reality, ok := tls["reality"].(map[string]any); ok && len(reality) > 0 && !singBoxOutboundSupportsReality(protocol) {
			return fmt.Errorf("sing-box outbound %q protocol %s does not support REALITY", tag, protocol)
		}
	}
	if singBoxOutboundRequiresTLS(protocol) {
		enabled, _ := tls["enabled"].(bool)
		if !hasTLS || !enabled {
			return fmt.Errorf("sing-box outbound %q %s requires TLS", tag, protocol)
		}
	}

	transport, ok := outbound["transport"].(map[string]any)
	if !ok || len(transport) == 0 {
		return nil
	}
	if !knownProtocol {
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

func stringSliceLength(value any) int {
	switch values := value.(type) {
	case []string:
		return len(values)
	case []any:
		return len(values)
	default:
		return 0
	}
}

func validateWireGuardReserved(peer map[string]any, tag string, index int) error {
	value, exists := peer["reserved"]
	if !exists {
		return nil
	}
	reserved := compatIntSlice(value)
	if len(reserved) != 3 {
		return fmt.Errorf("sing-box WireGuard endpoint %q peer %d reserved must contain exactly 3 bytes", tag, index+1)
	}
	for _, part := range reserved {
		if part < 0 || part > 255 {
			return fmt.Errorf("sing-box WireGuard endpoint %q peer %d has invalid reserved byte %d", tag, index+1, part)
		}
	}
	return nil
}

func isKnownSingBoxEndpoint(protocol string) bool {
	switch protocol {
	case "wireguard", "tailscale", "openconnect", "openvpn-client", "openvpn-server", "masque-client", "masque-server":
		return true
	default:
		return false
	}
}

func validateWireGuardEndpoint(endpoint map[string]any, tag string) error {
	if stringSliceLength(endpoint["address"]) == 0 {
		return fmt.Errorf("sing-box WireGuard endpoint %q requires an interface address", tag)
	}
	if rawString(endpoint, "private_key") == "" {
		return fmt.Errorf("sing-box WireGuard endpoint %q requires a private key", tag)
	}
	peers, ok := endpoint["peers"].([]map[string]any)
	if !ok {
		rawPeers, rawOK := endpoint["peers"].([]any)
		if !rawOK || len(rawPeers) == 0 {
			return fmt.Errorf("sing-box WireGuard endpoint %q requires at least one peer", tag)
		}
		peers = make([]map[string]any, 0, len(rawPeers))
		for _, item := range rawPeers {
			peer, peerOK := item.(map[string]any)
			if !peerOK {
				return fmt.Errorf("sing-box WireGuard endpoint %q contains an invalid peer", tag)
			}
			peers = append(peers, peer)
		}
	}
	if len(peers) == 0 {
		return fmt.Errorf("sing-box WireGuard endpoint %q requires at least one peer", tag)
	}
	for i, peer := range peers {
		address := strings.TrimSpace(rawString(peer, "address"))
		port := rawInt(peer, "port")
		if address == "" {
			if port != 0 {
				return fmt.Errorf("sing-box WireGuard endpoint %q peer %d has a port without an address", tag, i+1)
			}
		} else if port < 1 || port > 65535 {
			return fmt.Errorf("sing-box WireGuard endpoint %q peer %d has an invalid port", tag, i+1)
		}
		if rawString(peer, "public_key") == "" {
			return fmt.Errorf("sing-box WireGuard endpoint %q peer %d requires a public key", tag, i+1)
		}
		if stringSliceLength(peer["allowed_ips"]) == 0 {
			return fmt.Errorf("sing-box WireGuard endpoint %q peer %d requires allowed IPs", tag, i+1)
		}
		if err := validateWireGuardReserved(peer, tag, i); err != nil {
			return err
		}
	}
	return nil
}

func validateSingBoxEndpoint(endpoint map[string]any) error {
	protocol := strings.ToLower(strings.TrimSpace(rawString(endpoint, "type")))
	tag := rawString(endpoint, "tag")
	switch {
	case protocol == "":
		return fmt.Errorf("sing-box endpoint %q has an empty type", tag)
	case protocol == "wireguard":
		return validateWireGuardEndpoint(endpoint, tag)
	case isKnownSingBoxEndpoint(protocol):
		return nil
	case isKnownSingBoxOutbound(protocol):
		return fmt.Errorf("sing-box endpoint %q uses outbound type %q", tag, protocol)
	default:
		// Unknown endpoint types are passed through so newer sing-box releases can
		// be used before the panel learns their schema. sing-box check validates
		// the final configuration before the process starts.
		return nil
	}
}

func hasDNSServerTag(dns map[string]any, tag string) bool {
	if dns == nil || tag == "" {
		return false
	}
	switch servers := dns["servers"].(type) {
	case []map[string]any:
		for _, server := range servers {
			if rawString(server, "tag") == tag {
				return true
			}
		}
	case []any:
		for _, item := range servers {
			server, ok := item.(map[string]any)
			if ok && rawString(server, "tag") == tag {
				return true
			}
		}
	}
	return false
}

func (c *Config) MarshalJSON() ([]byte, error) {
	if c == nil {
		return []byte("null"), nil
	}

	clone := configJSON(*c)
	clone.Outbounds = make([]map[string]any, 0, len(c.Outbounds))
	clone.Endpoints = make([]map[string]any, 0, len(c.Endpoints))
	clone.Route = maps.Clone(c.Route)
	if hasDNSServerTag(c.DNS, "local") {
		if clone.Route == nil {
			clone.Route = map[string]any{}
		}
		if _, exists := clone.Route["default_domain_resolver"]; !exists {
			clone.Route["default_domain_resolver"] = "local"
		}
	}
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
		if err := validateSingBoxEndpoint(endpoint); err != nil {
			return nil, err
		}
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
