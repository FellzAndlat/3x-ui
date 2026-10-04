package sub

import (
	"fmt"
	"strconv"
	"strings"
)

// clashProxyFromExternal converts a pasted share link into a mihomo/Clash proxy
// entry, or nil when Clash can't represent it — the same gate getProxies runs.
func (s *SubClashService) clashProxyFromExternal(rawLink, name string) map[string]any {
	ob := parseExternalLink(rawLink)
	if ob == nil {
		return nil
	}
	protocol, _ := ob["protocol"].(string)
	settings, _ := ob["settings"].(map[string]any)
	stream, _ := ob["streamSettings"].(map[string]any)
	if stream == nil {
		stream = map[string]any{}
	}
	if settings == nil {
		return nil
	}

	proxy := map[string]any{"name": name, "udp": true}

	switch protocol {
	case "vmess":
		vnext, _ := settings["vnext"].([]any)
		if len(vnext) == 0 {
			return nil
		}
		vn, _ := vnext[0].(map[string]any)
		users, _ := vn["users"].([]any)
		if vn == nil || len(users) == 0 {
			return nil
		}
		user, _ := users[0].(map[string]any)
		proxy["type"] = "vmess"
		proxy["server"] = fmt.Sprint(vn["address"])
		proxy["port"] = clashInt(vn["port"])
		proxy["uuid"] = fmt.Sprint(user["id"])
		proxy["alterId"] = clashInt(user["alterId"])
		cipher, _ := user["security"].(string)
		if cipher == "" {
			cipher = "auto"
		}
		proxy["cipher"] = cipher
	case "vless":
		proxy["type"] = "vless"
		proxy["server"] = fmt.Sprint(settings["address"])
		proxy["port"] = clashInt(settings["port"])
		proxy["uuid"] = fmt.Sprint(settings["id"])
		if encryption, _ := settings["encryption"].(string); encryption != "" && encryption != "none" {
			proxy["encryption"] = encryption
		}
		if flow, _ := settings["flow"].(string); flow != "" {
			proxy["flow"] = flow
		}
	case "trojan":
		server := firstServer(settings)
		if server == nil {
			return nil
		}
		proxy["type"] = "trojan"
		proxy["server"] = fmt.Sprint(server["address"])
		proxy["port"] = clashInt(server["port"])
		proxy["password"] = fmt.Sprint(server["password"])
	case "shadowsocks":
		server := firstServer(settings)
		if server == nil {
			server = settings
		}
		method, _ := server["method"].(string)
		if method == "" {
			return nil
		}
		proxy["type"] = "ss"
		proxy["server"] = fmt.Sprint(server["address"])
		proxy["port"] = clashInt(server["port"])
		proxy["cipher"] = method
		proxy["password"] = fmt.Sprint(server["password"])
		// No early return: the shared transport/security tail is what drops an
		// obfs node Clash cannot express, exactly as buildProxy does for inbounds.
	case "hysteria":
		return clashHysteriaFromExternal(settings, stream, name)
	case "wireguard":
		return clashWireguardFromExternal(settings, name)
	default:
		return nil
	}

	network, _ := stream["network"].(string)
	if !s.applyTransport(proxy, network, stream) {
		return nil
	}
	security, _ := stream["security"].(string)
	if !s.applySecurity(proxy, security, stream) {
		return nil
	}
	return proxy
}

func firstServer(settings map[string]any) map[string]any {
	servers, _ := settings["servers"].([]any)
	if len(servers) == 0 {
		return nil
	}
	server, _ := servers[0].(map[string]any)
	return server
}

func clashHysteriaFromExternal(settings, stream map[string]any, name string) map[string]any {
	hy, _ := stream["hysteriaSettings"].(map[string]any)
	auth := ""
	if hy != nil {
		auth, _ = hy["auth"].(string)
	}
	if auth == "" {
		return nil
	}
	proxy := map[string]any{
		"name":     name,
		"type":     "hysteria2",
		"server":   fmt.Sprint(settings["address"]),
		"port":     clashInt(settings["port"]),
		"password": auth,
		"udp":      true,
	}
	if !(&SubClashService{}).applySecurity(proxy, "tls", stream) {
		return nil
	}
	delete(proxy, "tls") // Hysteria2 always uses QUIC TLS.
	finalmask, _ := stream["finalmask"].(map[string]any)
	masks, _ := finalmask["udp"].([]any)
	for _, item := range masks {
		mask, _ := item.(map[string]any)
		settings, _ := mask["settings"].(map[string]any)
		switch mask["type"] {
		case "salamander":
			password, _ := settings["password"].(string)
			if password == "" {
				return nil
			}
			proxy["obfs"], proxy["obfs-password"] = "salamander", password
			if size, _ := settings["packetSize"].(string); size != "" {
				if parseHysteriaPacketSize(size) == "" {
					return nil
				}
				min, max := splitHysteriaPacketSize(size)
				proxy["obfs"] = "gecko"
				proxy["obfs-min-packet-size"], _ = strconv.Atoi(min)
				proxy["obfs-max-packet-size"], _ = strconv.Atoi(max)
			}
		case "udphop":
			if settings["mode"] != "intervalremote" {
				return nil
			}
			if interval, _ := settings["interval"].(string); interval != "" {
				parts := strings.Split(interval, "-")
				if len(parts) > 2 {
					return nil
				}
				min, err := strconv.Atoi(parts[0])
				if err != nil || min <= 0 {
					return nil
				}
				if len(parts) == 2 {
					max, err := strconv.Atoi(parts[1])
					if err != nil || max < min {
						return nil
					}
				}
				proxy["hop-interval"] = interval
			}
		default:
			return nil
		}
	}
	if ports := hysteriaHopPorts(stream); ports != "" {
		proxy["ports"] = ports
	}
	return proxy
}

func clashWireguardFromExternal(settings map[string]any, name string) map[string]any {
	peers, _ := settings["peers"].([]any)
	if len(peers) == 0 {
		return nil
	}
	peer, _ := peers[0].(map[string]any)
	if peer == nil {
		return nil
	}
	host, port := splitClashHostPort(fmt.Sprint(peer["endpoint"]))
	if host == "" || port == 0 {
		return nil
	}
	proxy := map[string]any{
		"name":   name,
		"type":   "wireguard",
		"server": host,
		"port":   port,
		"udp":    true,
	}
	if sk, _ := settings["secretKey"].(string); sk != "" {
		proxy["private-key"] = sk
	}
	if pk, _ := peer["publicKey"].(string); pk != "" {
		proxy["public-key"] = pk
	}
	if psk, _ := peer["preSharedKey"].(string); psk != "" {
		proxy["pre-shared-key"] = psk
	}
	for _, addr := range clashStringList(settings["address"]) {
		ip := stripCIDR(addr)
		if strings.Contains(ip, ":") {
			proxy["ipv6"] = ip
		} else {
			proxy["ip"] = ip
		}
	}
	if mtu := clashInt(settings["mtu"]); mtu > 0 {
		proxy["mtu"] = mtu
	}
	if keepalive := clashInt(peer["keepAlive"]); keepalive > 0 {
		proxy["persistent-keepalive"] = keepalive
	}
	if reserved, exists := settings["reserved"]; exists {
		proxy["reserved"] = reserved
	}
	if allowed := clashStringList(peer["allowedIPs"]); len(allowed) > 0 {
		proxy["allowed-ips"] = allowed
	}
	return proxy
}

func clashInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	default:
		return 0
	}
}

func clashStringList(v any) []string {
	switch x := v.(type) {
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	case string:
		if x == "" {
			return nil
		}
		return strings.Split(x, ",")
	default:
		return nil
	}
}

func stripCIDR(addr string) string {
	if before, _, ok := strings.Cut(addr, "/"); ok {
		return before
	}
	return addr
}

func splitClashHostPort(endpoint string) (string, int) {
	endpoint = strings.TrimSpace(endpoint)
	i := strings.LastIndex(endpoint, ":")
	if i < 0 {
		return endpoint, 0
	}
	host := strings.Trim(endpoint[:i], "[]")
	port, _ := strconv.Atoi(endpoint[i+1:])
	return host, port
}
