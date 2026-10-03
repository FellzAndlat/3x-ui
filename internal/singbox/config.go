package singbox

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
 "github.com/SawaMEN/3x-ui/v3/internal/snell"
)

type Config struct {
	Log          map[string]any   `json:"log,omitempty"`
	DNS          map[string]any   `json:"dns,omitempty"`
	Inbounds     []map[string]any `json:"inbounds,omitempty"`
	Outbounds    []map[string]any `json:"outbounds,omitempty"`
	Route        map[string]any   `json:"route,omitempty"`
	Endpoints    []map[string]any `json:"endpoints,omitempty"`
	Services     []map[string]any `json:"services,omitempty"`
	Experimental map[string]any   `json:"experimental,omitempty"`
}

func NewConfig() *Config {
	return &Config{
		Log:       map[string]any{"level": "info", "output": filepath.Join(filepath.Dir(GetConfigPath()), "sing-box.log")},
		DNS:       map[string]any{"servers": []map[string]any{{"type": "local", "tag": "local"}}, "final": "local", "strategy": "ipv4_only"},
		Inbounds:  []map[string]any{},
		Outbounds: []map[string]any{{"type": "direct", "tag": "direct"}, {"type": "block", "tag": "block"}},
		Route:     map[string]any{"final": "direct"},
	}
}

func (c *Config) Marshal() ([]byte, error) {
	seen := make(map[string]struct{}, len(c.Outbounds))
	for _, outbound := range c.Outbounds {
		tag := rawString(outbound, "tag")
		if tag != "" {
			if _, exists := seen[tag]; exists {
				return nil, fmt.Errorf("duplicate sing-box outbound tag %q", tag)
			}
			seen[tag] = struct{}{}
		}
	}
	return json.MarshalIndent(c, "", "  ")
}

func TranslateXrayOutbound(raw map[string]any) (map[string]any, error) {
	protocol := strings.ToLower(strings.TrimSpace(rawString(raw, "protocol")))
	tag := rawString(raw, "tag")
	if tag == "" {
		return nil, fmt.Errorf("outbound tag is empty")
	}
	out := map[string]any{"tag": tag}
	switch protocol {
	case "freedom":
		out["type"] = "direct"
	case "blackhole":
		out["type"] = "block"
	case "socks", "http", "shadowsocks", "vmess", "vless", "trojan", "hysteria", "hysteria2", "tuic":
		out["type"] = protocol
	case "wireguard":
		return nil, fmt.Errorf("outbound %q is migrated to a sing-box WireGuard endpoint", tag)
	default:
		return translateNativeSingBoxOutbound(raw, protocol, tag)
	}
	settings := rawObject(raw, "settings")
	streamSettings := rawObject(raw, "streamSettings")
	if err := validateSingleOutboundTarget(settings, protocol, tag); err != nil {
		return nil, err
	}
	switch protocol {
	case "socks", "http":
		server := firstObject(settings, "servers")
		if server == nil {
			return nil, fmt.Errorf("outbound %q has no server", tag)
		}
		if address := rawString(server, "address"); address != "" {
			out["server"] = address
		} else {
			return nil, fmt.Errorf("outbound %q has an empty server address", tag)
		}
		port := rawInt(server, "port")
		if port <= 0 {
			return nil, fmt.Errorf("outbound %q has an invalid server port", tag)
		}
		out["server_port"] = port
		if users, _ := server["users"].([]any); len(users) > 0 {
			user, _ := users[0].(map[string]any)
			if username := rawString(user, "user"); username != "" {
				out["username"] = username
			}
			if password := rawString(user, "pass"); password != "" {
				out["password"] = password
			} else if password := rawString(user, "password"); password != "" {
				out["password"] = password
			}
		}
	case "shadowsocks":
		server := firstObject(settings, "servers")
		if server == nil {
			return nil, fmt.Errorf("outbound %q has no server", tag)
		}
		if address := rawString(server, "address"); address != "" {
			out["server"] = address
		} else {
			return nil, fmt.Errorf("outbound %q has an empty server address", tag)
		}
		port := rawInt(server, "port")
		if port <= 0 {
			return nil, fmt.Errorf("outbound %q has an invalid server port", tag)
		}
		out["server_port"] = port
		for _, key := range []string{"method", "password"} {
			if value := rawString(server, key); value != "" {
				out[key] = value
			}
		}
	case "vmess", "vless":
		server := firstObject(settings, "vnext")
		address := ""
		port := 0
		user := map[string]any(nil)
		if server != nil {
			address = rawString(server, "address")
			port = rawInt(server, "port")
			if users, _ := server["users"].([]any); len(users) > 0 {
				user, _ = users[0].(map[string]any)
			}
		} else if protocol == "vless" || protocol == "vmess" {
			// The panel's modern VLESS/VMess forms store a single target flat in
			// settings rather than using Xray's legacy vnext wrapper.
			address = rawString(settings, "address")
			port = rawInt(settings, "port")
			user = settings
		} else {
			return nil, fmt.Errorf("outbound %q has no vnext server", tag)
		}
		if address == "" {
			return nil, fmt.Errorf("outbound %q has an empty server address", tag)
		}
		if port <= 0 {
			return nil, fmt.Errorf("outbound %q has an invalid server port", tag)
		}
		if user == nil {
			return nil, fmt.Errorf("outbound %q has no user", tag)
		}
		out["server"] = address
		out["server_port"] = port
		id := rawString(user, "id")
		if id == "" {
			return nil, fmt.Errorf("outbound %q has an empty user id", tag)
		}
		out["uuid"] = id
		if protocol == "vmess" {
			security := rawString(user, "security")
			if security == "" {
				security = "auto"
			}
			out["security"] = security
			out["alter_id"] = rawInt(user, "alterId")
		} else if encryption := rawString(user, "encryption"); encryption != "" && encryption != "none" {
			return nil, fmt.Errorf("outbound %q uses unsupported VLESS encryption %q", tag, encryption)
		}
		if protocol == "vless" {
			if flow := rawString(user, "flow"); flow != "" {
				out["flow"] = flow
			}
		}
	case "trojan":
		server := firstObject(settings, "servers")
		if server == nil {
			return nil, fmt.Errorf("outbound %q has no server", tag)
		}
		if address := rawString(server, "address"); address != "" {
			out["server"] = address
		} else {
			return nil, fmt.Errorf("outbound %q has an empty server address", tag)
		}
		port := rawInt(server, "port")
		if port <= 0 {
			return nil, fmt.Errorf("outbound %q has an invalid server port", tag)
		}
		out["server_port"] = port
		if password := rawString(server, "password"); password != "" {
			out["password"] = password
		} else {
			return nil, fmt.Errorf("outbound %q has an empty password", tag)
		}
	case "hysteria", "hysteria2":
		server := firstObject(settings, "servers")
		address := ""
		port := 0
		if server != nil {
			address = rawString(server, "address")
			port = rawInt(server, "port")
		} else {
			// The panel's Hysteria form stores the target as flat settings.
			address = rawString(settings, "address")
			port = rawInt(settings, "port")
		}
		if address == "" {
			return nil, fmt.Errorf("outbound %q has an empty server address", tag)
		}
		hySettings := rawObject(streamSettings, "hysteriaSettings")
		serverPorts := compatStringSlice(hySettings["server_ports"])
		if len(serverPorts) == 0 {
			serverPorts = compatStringSlice(hySettings["serverPorts"])
		}
		out["server"] = address
		if len(serverPorts) > 0 {
			out["server_ports"] = serverPorts
		} else {
			if port <= 0 {
				return nil, fmt.Errorf("outbound %q has an invalid server port", tag)
			}
			out["server_port"] = port
		}
		if protocol == "hysteria2" {
			password := rawString(settings, "password")
			if password == "" {
				password = rawString(server, "password")
			}
			if password == "" {
				password = rawString(hySettings, "auth")
			}
			if password != "" {
				out["password"] = password
			}
		}
	case "tuic":
		server := firstObject(settings, "servers")
		if server == nil {
			return nil, fmt.Errorf("outbound %q has no server", tag)
		}
		address := rawString(server, "address")
		port := rawInt(server, "port")
		if address == "" {
			return nil, fmt.Errorf("outbound %q has an empty server address", tag)
		}
		if port <= 0 {
			return nil, fmt.Errorf("outbound %q has an invalid server port", tag)
		}
		out["server"] = address
		out["server_port"] = port
		uuid := rawString(server, "uuid")
		if uuid == "" {
			uuid = rawString(server, "id")
		}
		if uuid != "" {
			out["uuid"] = uuid
		}
		if password := rawString(server, "password"); password != "" {
			out["password"] = password
		}
		for _, key := range []string{"congestion_control", "udp_relay_mode", "zero_rtt_handshake", "heartbeat"} {
			if value, ok := settings[key]; ok {
				out[key] = value
			}
		}
	}
	singProtocol := protocol
	if protocol == "hysteria" {
		hysteriaSettings := rawObject(streamSettings, "hysteriaSettings")
		version := rawInt(hysteriaSettings, "version")
		if version == 0 {
			version = 2
		}
		switch version {
		case 2:
			out["type"] = "hysteria2"
			auth := rawString(hysteriaSettings, "auth")
			if auth == "" {
				auth = rawString(firstObject(settings, "servers"), "password")
			}
			if auth == "" {
				auth = rawString(settings, "password")
			}
			if auth != "" {
				out["password"] = auth
			}
			singProtocol = "hysteria2"
		case 1:
			auth := rawString(hysteriaSettings, "auth")
			if auth == "" {
				auth = rawString(firstObject(settings, "servers"), "auth")
			}
			if auth != "" {
				out["auth_str"] = auth
			}
		default:
			return nil, fmt.Errorf("outbound %q has unsupported Hysteria version %d", tag, version)
		}
	}
	if singProtocol == "hysteria" {
		hySettings := rawObject(streamSettings, "hysteriaSettings")
		if value, ok := hySettings["hop_interval"]; ok {
			out["hop_interval"] = value
		} else if value, ok := hySettings["hopInterval"]; ok {
			out["hop_interval"] = value
		}
	}
	if singProtocol == "hysteria2" {
		hySettings := rawObject(streamSettings, "hysteriaSettings")
		for _, key := range []string{"hop_interval", "hop_interval_max", "disable_chrome_parrot"} {
			if value, ok := hySettings[key]; ok {
				out[key] = value
			}
		}
		if _, ok := out["hop_interval"]; !ok {
			if value, ok := hySettings["hopInterval"]; ok {
				out["hop_interval"] = value
			}
		}
		if _, ok := out["hop_interval_max"]; !ok {
			if value, ok := hySettings["hopIntervalMax"]; ok {
				out["hop_interval_max"] = value
			}
		}
		if _, ok := out["disable_chrome_parrot"]; !ok {
			if value, ok := hySettings["disableChromeParrot"]; ok {
				out["disable_chrome_parrot"] = value
			}
		}
	}
	if err := translateStream(out, singProtocol, streamSettings, false); err != nil {
		return nil, err
	}
	if err := applyXrayOutboundCompatibility(out, raw, streamSettings); err != nil {
		return nil, err
	}
	return out, nil
}

func validateSingleOutboundTarget(settings map[string]any, protocol, tag string) error {
	key := "servers"
	switch protocol {
	case "vmess", "vless":
		key = "vnext"
	case "socks", "http", "shadowsocks", "trojan", "hysteria", "hysteria2", "tuic":
	default:
		return nil
	}
	targets, ok := settings[key].([]any)
	if !ok || len(targets) == 0 {
		return nil
	}
	if len(targets) != 1 {
		return fmt.Errorf("outbound %q has %d %s targets; sing-box supports one per outbound", tag, len(targets), key)
	}
	if protocol == "socks" || protocol == "http" || protocol == "vmess" || protocol == "vless" {
		server, _ := targets[0].(map[string]any)
		users, _ := server["users"].([]any)
		if len(users) > 1 {
			return fmt.Errorf("outbound %q has %d users; sing-box supports one per outbound", tag, len(users))
		}
	}
	return nil
}

// TranslateXrayWireGuardEndpoint converts the panel's Xray WireGuard
// outbound settings to the native sing-box endpoint format.
func TranslateXrayWireGuardEndpoint(raw map[string]any) (map[string]any, error) {
	tag := rawString(raw, "tag")
	if tag == "" {
		return nil, fmt.Errorf("wireguard outbound tag is empty")
	}
	settings := rawObject(raw, "settings")
	privateKey := strings.TrimSpace(rawString(settings, "secretKey"))
	if privateKey == "" {
		return nil, fmt.Errorf("wireguard outbound %q has no private key", tag)
	}
	addresses, err := normalizeWireGuardLocalAddresses(compatStringSlice(settings["address"]))
	if err != nil {
		return nil, fmt.Errorf("wireguard outbound %q: %w", tag, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("wireguard outbound %q has no local address", tag)
	}
	peerRaw, _ := settings["peers"].([]any)
	if len(peerRaw) == 0 {
		return nil, fmt.Errorf("wireguard outbound %q has no peers", tag)
	}
	peers := make([]map[string]any, 0, len(peerRaw))
	for i, item := range peerRaw {
		peer, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("wireguard outbound %q peer %d is invalid", tag, i+1)
		}
		publicKey := strings.TrimSpace(rawString(peer, "publicKey"))
		if publicKey == "" {
			return nil, fmt.Errorf("wireguard outbound %q peer %d has no public key", tag, i+1)
		}
		host, port, err := parseWireGuardEndpoint(rawString(peer, "endpoint"))
		if err != nil {
			return nil, fmt.Errorf("wireguard outbound %q peer %d: %w", tag, i+1, err)
		}
		allowed, err := normalizeWireGuardAddresses(rawStrings(peer, "allowedIPs"))
		if err != nil {
			return nil, fmt.Errorf("wireguard outbound %q peer %d: %w", tag, i+1, err)
		}
		if len(allowed) == 0 {
			allowed = []string{"0.0.0.0/0", "::/0"}
		}
		p := map[string]any{
			"address":     host,
			"port":        port,
			"public_key":  publicKey,
			"allowed_ips": allowed,
		}
		psk := strings.TrimSpace(rawString(peer, "preSharedKey"))
		if psk == "" {
			psk = strings.TrimSpace(rawString(peer, "psk"))
		}
		if psk != "" {
			p["pre_shared_key"] = psk
		}
		if keepAlive := rawInt(peer, "keepAlive"); keepAlive > 0 {
			p["persistent_keepalive_interval"] = keepAlive
		}
		peers = append(peers, p)
	}
	endpoint := map[string]any{
		"type":        "wireguard",
		"tag":         tag,
		"address":     addresses,
		"private_key": privateKey,
		"peers":       peers,
	}
	if mtu := rawInt(settings, "mtu"); mtu > 0 {
		endpoint["mtu"] = mtu
	}
	reserved, err := normalizeWireGuardReserved(settings["reserved"])
	if err != nil {
		return nil, fmt.Errorf("wireguard outbound %q: %w", tag, err)
	}
	if len(reserved) > 0 {
		for _, peer := range peers {
			peer["reserved"] = reserved
		}
	}
	if err := applyXrayWireGuardEndpointCompatibility(endpoint, raw); err != nil {
		return nil, err
	}
	return endpoint, nil
}

func parseWireGuardEndpoint(value string) (string, int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, fmt.Errorf("endpoint is empty")
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.Hostname() == "" || u.Port() == "" {
			return "", 0, fmt.Errorf("invalid endpoint %q", value)
		}
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", 0, fmt.Errorf("invalid endpoint port %q", u.Port())
		}
		return u.Hostname(), port, nil
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", 0, fmt.Errorf("endpoint must be host:port or [IPv6]:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("invalid endpoint port %q", portText)
	}
	return host, port, nil
}

func normalizeWireGuardLocalAddresses(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if ip, network, err := net.ParseCIDR(value); err == nil {
			ones, _ := network.Mask.Size()
			value = ip.String() + "/" + strconv.Itoa(ones)
		} else if ip := net.ParseIP(value); ip != nil {
			if ip.To4() != nil {
				value = ip.String() + "/32"
			} else {
				value = ip.String() + "/128"
			}
		} else {
			return nil, fmt.Errorf("invalid address %q", value)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

func normalizeWireGuardAddresses(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(value); err == nil {
			value = network.String()
		} else if ip := net.ParseIP(value); ip != nil {
			if ip.To4() != nil {
				value = ip.String() + "/32"
			} else {
				value = ip.String() + "/128"
			}
		} else {
			return nil, fmt.Errorf("invalid address %q", value)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

func normalizeWireGuardReserved(v any) ([]int, error) {
	reserved := compatIntSlice(v)
	if len(reserved) == 0 {
		return nil, nil
	}
	if len(reserved) != 3 {
		return nil, fmt.Errorf("reserved must contain exactly 3 bytes")
	}
	for _, value := range reserved {
		if value < 0 || value > 255 {
			return nil, fmt.Errorf("reserved byte %d is outside 0..255", value)
		}
	}
	return reserved, nil
}

func compatIntSlice(v any) []int {
	switch values := v.(type) {
	case []any:
		out := make([]int, 0, len(values))
		for _, value := range values {
			switch n := value.(type) {
			case float64:
				out = append(out, int(n))
			case int:
				out = append(out, n)
			case string:
				parsed, err := strconv.Atoi(strings.TrimSpace(n))
				if err != nil {
					return nil
				}
				out = append(out, parsed)
			}
		}
		return out
	case []int:
		return append([]int(nil), values...)
	default:
		return nil
	}
}

func rawBool(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func TranslateXrayInbound(raw map[string]any) (map[string]any, error) {
	protocol := strings.ToLower(strings.TrimSpace(rawString(raw, "protocol")))
	if protocol == "tunnel" || protocol == "wireguard" || protocol == "mtproto" || protocol == "amneziawg" || protocol == "tuic" || protocol == "mieru" || protocol == "pingtunnel" || protocol == "trusttunnel" {
		return nil, fmt.Errorf("sing-box does not support Xray inbound protocol %q", protocol)
	}
	out := map[string]any{"tag": rawString(raw, "tag")}
	settings := rawObject(raw, "settings")
	stream := rawObject(raw, "streamSettings")
	singProtocol := protocol
	if protocol == "hysteria" {
		version := rawInt(rawObject(stream, "hysteriaSettings"), "version")
		if version == 0 {
			version = 2
		}
		if version == 2 {
			singProtocol = "hysteria2"
		}
	}
	if protocol == "naive" {
		if network := rawString(settings, "network"); network != "" {
			if network != "tcp" && network != "udp" {
				return nil, fmt.Errorf("inbound %q has invalid NaiveProxy network %q", rawString(raw, "tag"), network)
			}
			out["network"] = network
		}
		if network := rawString(settings, "network"); network == "udp" {
			if cc := rawString(settings, "quicCongestionControl"); cc != "" {
				out["quic_congestion_control"] = cc
			}
		}
		tls := rawObject(settings, "tls")
		// NaiveProxy always uses TLS. Imported configs may omit the TLS object,
		// so synthesize the required block instead of generating an invalid
		// sing-box inbound.
		t := map[string]any{"enabled": true}
		if serverName := rawString(tls, "serverName"); serverName != "" {
			t["server_name"] = serverName
		}
		if cert := rawString(tls, "certificatePath"); cert != "" {
			t["certificate_path"] = cert
		}
		if key := rawString(tls, "keyPath"); key != "" {
			t["key_path"] = key
		}
		out["tls"] = t
	}
	if protocol == "snell" {
 data, err := json.Marshal(settings); if err != nil { return nil, err }
 s, err := snell.Parse(string(data)); if err != nil { return nil, err }
 out["version"], out["psk"] = s.Version, s.PSK
 if s.Version == 6 { if s.Mode != "" { out["mode"] = s.Mode } } else if s.ObfsMode != "" { out["obfs_mode"] = s.ObfsMode }
 }
 if protocol == "anytls" {
		tls := rawObject(settings, "tls")
		cert := strings.TrimSpace(rawString(tls, "certificatePath"))
		key := strings.TrimSpace(rawString(tls, "keyPath"))
		if cert == "" || key == "" {
			return nil, fmt.Errorf("inbound %q AnyTLS requires both certificatePath and keyPath", rawString(raw, "tag"))
		}
		t := map[string]any{"enabled": true, "certificate_path": cert, "key_path": key}
		if serverName := strings.TrimSpace(rawString(tls, "serverName")); serverName != "" {
			t["server_name"] = serverName
		}
		out["tls"] = t
		if padding := rawStrings(settings, "paddingScheme"); len(padding) > 0 {
			out["padding_scheme"] = padding
		}
	}
	if protocol == "shadowtls" {
		version := rawInt(settings, "version")
		if version == 0 {
			version = 3
		}
		if version != 3 {
			return nil, fmt.Errorf("inbound %q ShadowTLS requires protocol version 3", rawString(raw, "tag"))
		}

		wildcard := strings.TrimSpace(rawString(settings, "wildcardSni"))
		if wildcard != "" {
			if wildcard != "off" && wildcard != "authed" && wildcard != "all" {
				return nil, fmt.Errorf("inbound %q has invalid ShadowTLS wildcardSni %q", rawString(raw, "tag"), wildcard)
			}
		}

		handshake := rawObject(settings, "handshake")
		server := strings.TrimSpace(rawString(handshake, "server"))
		if server == "" {
			// Accept the common normalized alias used by imported/translated configs.
			server = strings.TrimSpace(rawString(handshake, "address"))
		}
		if server == "" && wildcard != "all" {
			server = "cloudflare.com"
		}
		serverPort := rawInt(handshake, "serverPort")
		if serverPort <= 0 {
			// Keep compatibility with configs that already use sing-box's snake_case
			// handshake field names.
			serverPort = rawInt(handshake, "server_port")
		}
		if serverPort <= 0 {
			serverPort = rawInt(handshake, "port")
		}
		if serverPort <= 0 {
			// ShadowTLS uses port 443 by default when no handshake port is configured.
			serverPort = 443
		}
		if (server == "" && wildcard != "all") || serverPort <= 0 || serverPort > 65535 {
			return nil, fmt.Errorf("inbound %q ShadowTLS requires a valid handshake server and port", rawString(raw, "tag"))
		}

		out["version"] = 3
		handshakeOut := map[string]any{
			"server_port": serverPort,
		}
		if server != "" {
			handshakeOut["server"] = server
			handshakeOut["domain_resolver"] = "local"
		}
		out["handshake"] = handshakeOut
		if named, ok := settings["handshakeForServerName"].(map[string]any); ok && len(named) > 0 {
			mapped := make(map[string]any, len(named))
			for rawName, entry := range named {
				name := strings.ToLower(strings.TrimSpace(rawName))
				config, ok := entry.(map[string]any)
				if !ok || name == "" || strings.TrimSpace(rawString(config, "server")) == "" {
					return nil, fmt.Errorf("inbound %q has invalid ShadowTLS handshake for SNI %q", rawString(raw, "tag"), rawName)
				}
				port := rawInt(config, "serverPort")
				if port == 0 {
					port = rawInt(config, "server_port")
				}
				if port == 0 {
					port = 443
				}
				if port < 1 || port > 65535 {
					return nil, fmt.Errorf("inbound %q has invalid ShadowTLS handshake port for SNI %q", rawString(raw, "tag"), rawName)
				}
				target := strings.TrimSpace(rawString(config, "server"))
				mapped[name] = map[string]any{"server": target, "server_port": port, "domain_resolver": "local"}
			}
			out["handshake_for_server_name"] = mapped
		}
		out["strict_mode"] = rawBool(settings, "strictMode")
		if wildcard != "" {
			out["wildcard_sni"] = wildcard
		}
		// ShadowTLS authenticates and unwraps a TCP stream; it does not carry a
		// destination. Deliver the stream to the encrypted Shadowsocks inbound.
		out["detour"] = shadowTLSInnerTag(rawString(raw, "tag"))
	}
	out["type"] = singProtocol
	if listen := rawString(raw, "listen"); listen != "" {
		out["listen"] = normalizeListen(listen)
	}
	if port := rawInt(raw, "port"); port > 0 {
		out["listen_port"] = port
	}
	if err := translateUsers(out, singProtocol, settings); err != nil {
		return nil, err
	}
	if protocol != "snell" && protocol != "naive" && protocol != "anytls" && protocol != "shadowtls" {
		if err := translateStream(out, singProtocol, stream, true); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func shadowTLSInnerTag(tag string) string {
	return "__shadowtls_ss_" + tag
}

// TranslateShadowTLSWrappedInbound keeps the actual proxy protocol as the
// injectable inner listener; ShadowTLS only owns the public TCP socket.
func TranslateShadowTLSWrappedInbound(raw map[string]any) (map[string]any, map[string]any, error) {
	protocol := model.Protocol(rawString(raw, "protocol"))
	if !model.SupportsShadowTLSTransport(protocol) {
		return nil, nil, fmt.Errorf("ShadowTLS cannot wrap %s", protocol)
	}
	settings := rawObject(raw, "settings")
	transport := rawObject(settings, "shadowTls")
	password := strings.TrimSpace(rawString(transport, "password"))
	if password == "" {
		return nil, nil, fmt.Errorf("inbound %q has no ShadowTLS transport password", rawString(raw, "tag"))
	}
	inner, err := TranslateXrayInbound(raw)
	if err != nil {
		return nil, nil, err
	}
	if protocol == model.HTTP || protocol == model.Mixed {
		accounts, _ := settings["accounts"].([]any)
		users := make([]map[string]any, 0, len(accounts))
		for _, entry := range accounts {
			account, _ := entry.(map[string]any)
			if user, pass := rawString(account, "user"), rawString(account, "pass"); user != "" && pass != "" {
				users = append(users, map[string]any{"username": user, "password": pass})
			}
		}
		inner["users"] = users
	}
	delete(inner, "listen")
	delete(inner, "listen_port")
	delete(inner, "network")
	if users, ok := inner["users"].([]map[string]any); ok {
		for _, user := range users {
			delete(user, "flow")
		}
	}
	outerRaw := map[string]any{
		"protocol": "shadowtls", "tag": "__shadowtls_transport_" + rawString(raw, "tag"),
		"listen": rawString(raw, "listen"), "port": raw["port"],
		"settings": map[string]any{
			"version": 3, "handshake": transport["handshake"],
			"handshakeForServerName": transport["handshakeForServerName"],
			"strictMode":             transport["strictMode"], "wildcardSni": transport["wildcardSni"],
			"clients": []any{map[string]any{"email": "panel", "password": password}},
		},
	}
	outer, err := TranslateXrayInbound(outerRaw)
	if err != nil {
		return nil, nil, err
	}
	outer["detour"] = rawString(inner, "tag")
	return outer, inner, nil
}

// TranslateShadowTLSInnerInbound builds the destination-carrying protocol for
// a ShadowTLS inbound. Shadowsocks 2022 encrypts the payload after ShadowTLS
// unwraps it; SOCKS would leave the tunneled traffic in cleartext.
func TranslateShadowTLSInnerInbound(raw map[string]any) (map[string]any, error) {
	settings := rawObject(raw, "settings")
	masterKey := rawString(settings, model.ShadowTLSInnerKeyField)
	if !model.ValidShadowTLSInnerKey(masterKey) {
		return nil, fmt.Errorf("ShadowTLS inbound %q has no valid inner Shadowsocks key", rawString(raw, "tag"))
	}
	clients, _ := settings["clients"].([]any)
	users := make([]map[string]any, 0, len(clients))
	for _, item := range clients {
		client, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := rawString(client, "email")
		password := rawString(client, "password")
		if name == "" || password == "" {
			return nil, fmt.Errorf("ShadowTLS inner Shadowsocks user requires a name and password")
		}
		users = append(users, map[string]any{"name": name, "password": model.ShadowTLSClientKey(name, password)})
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("ShadowTLS inbound %q has no users", rawString(raw, "tag"))
	}
	return map[string]any{
		"type": "shadowsocks", "tag": shadowTLSInnerTag(rawString(raw, "tag")),
		"listen": "127.0.0.1", "listen_port": 0, "method": "2022-blake3-aes-128-gcm",
		"password": masterKey, "users": users,
	}, nil
}

func translateUsers(out map[string]any, protocol string, settings map[string]any) error {
	clients, _ := settings["clients"].([]any)
	users := make([]map[string]any, 0, len(clients))
	for _, item := range clients {
		client, ok := item.(map[string]any)
		if !ok {
			continue
		}
		user := map[string]any{}
		if protocol != "naive" {
			if email, ok := client["email"].(string); ok && email != "" {
				user["name"] = email
			}
		}
		switch protocol {
		case "vless", "vmess":
			if id, ok := client["id"].(string); ok && id != "" {
				user["uuid"] = id
			}
			if flow, ok := client["flow"].(string); ok && flow != "" && protocol == "vless" {
				user["flow"] = flow
			}
		case "trojan", "shadowsocks", "hysteria2":
			if password, ok := client["password"].(string); ok && password != "" {
				user["password"] = password
			} else if auth := rawString(client, "auth"); auth != "" && protocol == "hysteria2" {
				user["password"] = auth
			}
		case "hysteria":
			if auth := rawString(client, "auth"); auth != "" {
				user["auth_str"] = auth
			}
		case "tuic":
			if id, ok := client["id"].(string); ok && id != "" {
				user["uuid"] = id
			}
			if password, ok := client["password"].(string); ok && password != "" {
				user["password"] = password
			}
		case "naive":
			if email := rawString(client, "email"); email != "" {
				user["username"] = email
			}
			if password := rawString(client, "password"); password != "" {
				user["password"] = password
			}
		case "snell":
 if password := rawString(client, "password"); password != "" { user["userkey"] = password } else { return fmt.Errorf("Snell client has no user key") }
 case "anytls", "shadowtls":
			if password := rawString(client, "password"); password != "" {
				user["password"] = password
			}
		}
		users = append(users, user)
	}
	switch protocol {
	case "vless", "vmess", "trojan", "naive", "hysteria", "hysteria2", "anytls", "shadowtls", "tuic", "http", "socks", "mixed":
		out["users"] = users
	default:
		if len(users) > 0 {
			out["users"] = users
		}
	}
	switch protocol {
	case "shadowsocks":
		if method := rawString(settings, "method"); method != "" {
			out["method"] = method
		}
		if password := rawString(settings, "password"); password != "" {
			out["password"] = password
		}
	case "http", "socks", "mixed":
		mapped := make([]map[string]any, 0, len(clients))
		for _, item := range clients {
			client, ok := item.(map[string]any)
			if !ok {
				continue
			}
			user := map[string]any{}
			if username := rawString(client, "email"); username != "" {
				user["username"] = username
			}
			if password := rawString(client, "password"); password != "" {
				user["password"] = password
			}
			if len(user) > 0 {
				mapped = append(mapped, user)
			}
		}
		if len(mapped) > 0 {
			out["users"] = mapped
		}
	}
	return nil
}

func translateStream(out map[string]any, protocol string, stream map[string]any, inbound bool) error {
	security := strings.ToLower(strings.TrimSpace(rawString(stream, "security")))
	if protocol == "hysteria" || protocol == "hysteria2" {
		if inbound {
			if security != "tls" {
				return fmt.Errorf("inbound %q uses %s without required TLS", rawString(out, "tag"), protocol)
			}
		} else {
			switch security {
			case "":
				out["tls"] = map[string]any{"enabled": true}
			case "tls":
			default:
				return fmt.Errorf("outbound %q uses %s with unsupported security %q", rawString(out, "tag"), protocol, security)
			}
		}
	}
	if len(stream) == 0 {
		if protocol == "hysteria" || protocol == "hysteria2" {
			return translateHysteriaStream(out, protocol, stream, inbound)
		}
		return nil
	}
	switch security {
	case "", "none":
	case "tls":
		tls := rawObject(stream, "tlsSettings")
		t := map[string]any{"enabled": true}
		if alpn := rawStrings(tls, "alpn"); len(alpn) > 0 {
			t["alpn"] = alpn
		}
		if !inbound {
			if insecure, ok := tls["allowInsecure"].(bool); ok {
				t["insecure"] = insecure
			}
			if fingerprint := rawString(tls, "fingerprint"); fingerprint != "" {
				t["utls"] = map[string]any{"enabled": true, "fingerprint": fingerprint}
			}
		}
		if serverName := rawString(tls, "serverName"); serverName != "" {
			t["server_name"] = serverName
		}
		if certs, ok := tls["certificates"].([]any); ok && len(certs) > 0 {
			for _, item := range certs {
				cert, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if path := rawString(cert, "certificateFile"); path != "" {
					t["certificate_path"] = path
				} else if value := compatStringSlice(cert["certificate"]); len(value) > 0 {
					t["certificate"] = value
				}
				if path := rawString(cert, "keyFile"); path != "" {
					t["key_path"] = path
				} else if value := compatStringSlice(cert["key"]); len(value) > 0 {
					t["key"] = value
				}
				break
			}
		}
		out["tls"] = t
	case "reality":
		reality := rawObject(stream, "realitySettings")
		tlsSettings := rawObject(stream, "tlsSettings")
		t := map[string]any{"enabled": true}
		r := map[string]any{"enabled": true}
		serverName := normalizeRealityServerName(rawString(tlsSettings, "serverName"))
		if serverName == "" {
			serverNames := rawStrings(reality, "serverNames")
			if len(serverNames) > 0 {
				serverName = normalizeRealityServerName(serverNames[0])
			}
		}
		if serverName == "" {
			serverName = normalizeRealityServerName(rawString(reality, "serverName"))
		}
		if serverName != "" {
			t["server_name"] = serverName
		}
		if inbound {
			dest := strings.TrimSpace(rawString(reality, "target"))
			if dest == "" {
				dest = strings.TrimSpace(rawString(reality, "dest"))
			}
			if dest == "" {
				dest = serverName
			}
			if dest == "" {
				return fmt.Errorf("inbound %q has REALITY enabled but no destination or serverName", rawString(out, "tag"))
			}
			host, port, err := parseRealityDestination(dest)
			if err != nil {
				return fmt.Errorf("inbound %q has invalid REALITY destination %q: %w", rawString(out, "tag"), dest, err)
			}
			r["handshake"] = map[string]any{
				"server":          host,
				"server_port":     port,
				"domain_resolver": "local",
			}
			if shortIDs, ok := reality["shortIds"].([]any); ok && len(shortIDs) > 0 {
				ids := make([]string, 0, len(shortIDs))
				for _, id := range shortIDs {
					if value, ok := id.(string); ok {
						ids = append(ids, value)
					}
				}
				if len(ids) > 0 {
					r["short_id"] = ids
				}
			} else if shortID := rawString(reality, "shortId"); shortID != "" {
				r["short_id"] = []string{shortID}
			}
			if rawString(reality, "privateKey") == "" {
				return fmt.Errorf("inbound %q has REALITY enabled but no private key", rawString(out, "tag"))
			}
			r["private_key"] = rawString(reality, "privateKey")
		} else {
			publicKey := rawString(reality, "publicKey")
			if publicKey == "" {
				publicKey = rawString(reality, "password")
			}
			if publicKey == "" {
				return fmt.Errorf("outbound %q has REALITY enabled but no public key", rawString(out, "tag"))
			}
			r["public_key"] = publicKey
			if shortID, exists := reality["shortId"].(string); exists {
				r["short_id"] = shortID
			} else if shortIDs, ok := reality["shortIds"].([]any); ok && len(shortIDs) > 0 {
				r["short_id"], _ = shortIDs[0].(string)
			} else {
				r["short_id"] = ""
			}
			fingerprint := rawString(reality, "fingerprint")
			if fingerprint == "" {
				fingerprint = rawString(tlsSettings, "fingerprint")
			}
			if fingerprint == "" {
				fingerprint = "chrome"
			}
			t["utls"] = map[string]any{"enabled": true, "fingerprint": fingerprint}
		}
		t["reality"] = r
		out["tls"] = t
	default:
		direction := "outbound"
		if inbound {
			direction = "inbound"
		}
		return fmt.Errorf("%s %q uses unsupported Xray stream security %q", direction, rawString(out, "tag"), security)
	}
	network := strings.ToLower(strings.TrimSpace(rawString(stream, "network")))
	if protocol == "hysteria2" || protocol == "hysteria" {
		if network == "hysteria" || network == "" {
			return translateHysteriaStream(out, protocol, stream, inbound)
		}
	}
	switch network {
	case "", "tcp", "raw":
		return nil
	case "ws":
		ws := rawObject(stream, "wsSettings")
		transport := map[string]any{"type": "ws"}
		if path := rawString(ws, "path"); path != "" {
			transport["path"] = path
		}
		headers, _ := ws["headers"].(map[string]any)
		headers = maps.Clone(headers)
		if host := rawString(ws, "host"); host != "" {
			if headers == nil {
				headers = map[string]any{}
			}
			for key := range headers {
				if strings.EqualFold(key, "Host") {
					delete(headers, key)
				}
			}
			headers["Host"] = host
		}
		if len(headers) > 0 {
			transport["headers"] = headers
		}
		if maxEarlyData := rawInt(ws, "maxEarlyData"); maxEarlyData > 0 {
			transport["max_early_data"] = maxEarlyData
		}
		if earlyDataHeaderName := rawString(ws, "earlyDataHeaderName"); earlyDataHeaderName != "" {
			transport["early_data_header_name"] = earlyDataHeaderName
		}
		out["transport"] = transport
	case "grpc":
		grpc := rawObject(stream, "grpcSettings")
		transport := map[string]any{"type": "grpc"}
		if name := rawString(grpc, "serviceName"); name != "" {
			transport["service_name"] = name
		}
		out["transport"] = transport
	case "http":
		httpSettings := rawObject(stream, "httpSettings")
		transport := map[string]any{"type": "http"}
		if hosts := rawStrings(httpSettings, "host"); len(hosts) > 0 {
			transport["host"] = hosts
		}
		if path := rawString(httpSettings, "path"); path != "" {
			transport["path"] = path
		}
		if method := rawString(httpSettings, "method"); method != "" {
			transport["method"] = method
		}
		if headers, ok := httpSettings["headers"].(map[string]any); ok && len(headers) > 0 {
			transport["headers"] = headers
		}
		out["transport"] = transport
	case "httpupgrade":
		upgrade := rawObject(stream, "httpupgradeSettings")
		transport := map[string]any{"type": "httpupgrade"}
		if host := rawString(upgrade, "host"); host != "" {
			transport["host"] = host
		}
		if path := rawString(upgrade, "path"); path != "" {
			transport["path"] = path
		}
		if headers, ok := upgrade["headers"].(map[string]any); ok && len(headers) > 0 {
			transport["headers"] = headers
		}
		out["transport"] = transport
	default:
		direction := "outbound"
		if inbound {
			direction = "inbound"
		}
		return fmt.Errorf("%s %q uses unsupported Xray transport %q", direction, rawString(out, "tag"), network)
	}
	return nil
}

func normalizeRealityServerName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		return strings.Trim(value, "[]")
	}
	return value
}

func parseRealityDestination(dest string) (string, int, error) {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", 0, fmt.Errorf("destination is empty")
	}
	if strings.Contains(dest, "://") {
		u, err := url.Parse(dest)
		if err != nil || u.Hostname() == "" {
			return "", 0, fmt.Errorf("invalid URL")
		}
		dest = u.Host
	}
	if host, port, err := net.SplitHostPort(dest); err == nil {
		n, err := strconv.Atoi(port)
		if err != nil || n <= 0 || n > 65535 {
			return "", 0, fmt.Errorf("invalid port")
		}
		host = strings.TrimSpace(host)
		if host == "" {
			return "", 0, fmt.Errorf("invalid host")
		}
		return host, n, nil
	}
	if strings.HasPrefix(dest, "[") && strings.HasSuffix(dest, "]") {
		dest = strings.Trim(dest, "[]")
	}
	if net.ParseIP(dest) != nil {
		return dest, 443, nil
	}
	if strings.Count(dest, ":") > 0 {
		return "", 0, fmt.Errorf("invalid host:port")
	}
	if strings.TrimSpace(dest) == "" {
		return "", 0, fmt.Errorf("invalid host")
	}
	return dest, 443, nil
}

func translateHysteriaStream(out map[string]any, protocol string, stream map[string]any, inbound bool) error {
	settings := rawObject(stream, "hysteriaSettings")
	version := rawInt(settings, "version")
	if version == 0 {
		version = 2
	}
	direction := "outbound"
	if inbound {
		direction = "inbound"
	}
	if protocol == "hysteria2" && version != 2 {
		return fmt.Errorf("%s %q uses Hysteria version %d, expected version 2 for hysteria2", direction, rawString(out, "tag"), version)
	}
	if protocol == "hysteria" && version != 1 {
		return fmt.Errorf("%s %q has inconsistent Hysteria version %d", direction, rawString(out, "tag"), version)
	}
	if protocol == "hysteria" {
		up := rawInt(settings, "up_mbps")
		if up <= 0 {
			up = rawInt(settings, "up")
		}
		down := rawInt(settings, "down_mbps")
		if down <= 0 {
			down = rawInt(settings, "down")
		}
		if up <= 0 || down <= 0 {
			return fmt.Errorf("%s %q Hysteria v1 requires positive up_mbps and down_mbps", direction, rawString(out, "tag"))
		}
		out["up_mbps"] = up
		out["down_mbps"] = down
	}
	if protocol == "hysteria2" {
		up := rawInt(settings, "up_mbps")
		if up <= 0 {
			up = rawInt(settings, "up")
		}
		if up > 0 {
			out["up_mbps"] = up
		}
		down := rawInt(settings, "down_mbps")
		if down <= 0 {
			down = rawInt(settings, "down")
		}
		if down > 0 {
			out["down_mbps"] = down
		}
		if inbound {
			if value, ok := settings["ignore_client_bandwidth"].(bool); ok {
				out["ignore_client_bandwidth"] = value
			} else if value, ok := settings["ignoreClientBandwidth"].(bool); ok {
				out["ignore_client_bandwidth"] = value
			}
		}
		profile := strings.ToLower(strings.TrimSpace(rawString(settings, "bbr_profile")))
		if profile == "" {
			profile = strings.ToLower(strings.TrimSpace(rawString(settings, "bbrProfile")))
		}
		if profile != "" {
			switch profile {
			case "conservative", "standard", "aggressive":
				out["bbr_profile"] = profile
			default:
				return fmt.Errorf("%s %q has invalid Hysteria2 bbr_profile %q", direction, rawString(out, "tag"), profile)
			}
		}
		if obfs := rawObject(settings, "obfs"); len(obfs) > 0 {
			kind := strings.ToLower(strings.TrimSpace(rawString(obfs, "type")))
			switch kind {
			case "salamander", "gecko":
				if strings.TrimSpace(rawString(obfs, "password")) == "" {
					return fmt.Errorf("%s %q Hysteria2 %s obfs requires a password", direction, rawString(out, "tag"), kind)
				}
				normalized := maps.Clone(obfs)
				normalized["type"] = kind
				if kind == "gecko" {
					if value := rawInt(obfs, "minPacketSize"); value > 0 {
						normalized["min_packet_size"] = value
						delete(normalized, "minPacketSize")
					}
					if value := rawInt(obfs, "maxPacketSize"); value > 0 {
						normalized["max_packet_size"] = value
						delete(normalized, "maxPacketSize")
					}
				}
				out["obfs"] = normalized
			case "":
			default:
				return fmt.Errorf("%s %q has invalid Hysteria2 obfs type %q", direction, rawString(out, "tag"), kind)
			}
		}
	}
	if masquerade := rawObject(settings, "masquerade"); inbound && len(masquerade) > 0 && protocol == "hysteria2" {
		m := map[string]any{}
		switch strings.ToLower(strings.TrimSpace(rawString(masquerade, "type"))) {
		case "proxy":
			m["type"] = "proxy"
			if value := rawString(masquerade, "url"); value != "" {
				m["url"] = value
			}
			if rewriteHost, ok := masquerade["rewriteHost"].(bool); ok {
				m["rewrite_host"] = rewriteHost
			} else if rewriteHost, ok := masquerade["rewrite_host"].(bool); ok {
				m["rewrite_host"] = rewriteHost
			} else if rawString(masquerade, "rewriteHost") == "true" {
				m["rewrite_host"] = true
			}
		case "file":
			m["type"] = "file"
			if dir := rawString(masquerade, "dir"); dir != "" {
				m["directory"] = dir
			} else if dir := rawString(masquerade, "directory"); dir != "" {
				m["directory"] = dir
			}
		case "string":
			m["type"] = "string"
			if content := rawString(masquerade, "content"); content != "" {
				m["content"] = content
			}
			if status := rawInt(masquerade, "statusCode"); status > 0 {
				m["status_code"] = status
			} else if status := rawInt(masquerade, "status_code"); status > 0 {
				m["status_code"] = status
			}
			if headers, ok := masquerade["headers"].(map[string]any); ok && len(headers) > 0 {
				m["headers"] = headers
			}
		case "":
		default:
			return fmt.Errorf("inbound %q has invalid Hysteria2 masquerade type %q", rawString(out, "tag"), rawString(masquerade, "type"))
		}
		if len(m) > 0 {
			out["masquerade"] = m
		}
	}
	return nil
}

func normalizeListen(value string) string {
	if value == "" {
		return "::"
	}
	return value
}

func rawString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func rawStrings(m map[string]any, key string) []string {
	values, _ := m[key].([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			result = append(result, strings.TrimSpace(s))
		}
	}
	return result
}

func rawInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func rawObject(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}

func firstObject(m map[string]any, key string) map[string]any {
	values, _ := m[key].([]any)
	if len(values) == 0 {
		if key == "servers" && strings.TrimSpace(rawString(m, "address")) != "" {
			return m
		}
		return nil
	}
	value, _ := values[0].(map[string]any)
	return value
}
