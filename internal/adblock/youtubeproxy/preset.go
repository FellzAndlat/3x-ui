package youtubeproxy

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
)

// RoutingPreset returns fragments for the native routing editors. No rule is
// applied until the operator chooses participating clients and installs the CA.
func RoutingPreset(core, listen string) ([]byte, error) {
	host, portRaw, err := net.SplitHostPort(listen)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 || ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("invalid loopback proxy address")
	}
	domains := []string{"youtube.com", "www.youtube.com", "m.youtube.com"}
	var data any
	switch core {
	case "xray":
		full := []string{}
		for _, d := range domains {
			full = append(full, "full:"+d)
		}
		data = map[string]any{"outbounds": []any{map[string]any{"tag": "youtube-server-filter", "protocol": "http", "settings": map[string]any{"servers": []any{map[string]any{"address": host, "port": port}}}}}, "routing": map[string]any{"rules": []any{map[string]any{"type": "field", "network": "udp", "port": "443", "domain": full, "outboundTag": "youtube-quic-block"}, map[string]any{"type": "field", "network": "tcp", "port": "443", "domain": full, "outboundTag": "youtube-server-filter"}}}}
		data.(map[string]any)["outbounds"] = append(data.(map[string]any)["outbounds"].([]any), map[string]any{"tag": "youtube-quic-block", "protocol": "blackhole", "settings": map[string]any{}})
	case "singbox":
		data = map[string]any{"outbounds": []any{map[string]any{"type": "http", "tag": "youtube-server-filter", "server": host, "server_port": port}}, "route": map[string]any{"rules": []any{map[string]any{"action": "sniff"}, map[string]any{"network": "udp", "port": 443, "domain": domains, "action": "reject"}, map[string]any{"network": "tcp", "port": 443, "domain": domains, "action": "route", "outbound": "youtube-server-filter"}}}}
	default:
		return nil, fmt.Errorf("unsupported core")
	}
	return json.MarshalIndent(data, "", "  ")
}
