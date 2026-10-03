package link

import (
	"fmt"
	"net/url"
	"strconv"
)

// Snell share links preserve both the deployment PSK and sing-box user key.
func parseSnell(link string) (*ParseResult, error) {
	u, err := url.Parse(link)
	if err != nil || u.User == nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid Snell link")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid Snell port")
	}
	q := u.Query()
	version, err := strconv.Atoi(q.Get("version"))
	if err != nil || (version != 4 && version != 5 && version != 6) {
		return nil, fmt.Errorf("invalid Snell version")
	}
	if version == 5 {
		version = 4
	}
	psk := u.User.Username()
	if psk == "" {
		return nil, fmt.Errorf("Snell requires PSK")
	}
	settings := map[string]any{"server": u.Hostname(), "server_port": port, "version": version, "psk": psk}
	for query, field := range map[string]string{"userkey": "userkey", "mode": "mode", "obfs": "obfs_mode", "obfs-host": "obfs_host"} {
		if value := q.Get(query); value != "" {
			settings[field] = value
		}
	}
	tag := u.Fragment
	if tag == "" {
		tag = "snell-out"
	}
	identity := "snell|" + u.Host + "|" + psk + "|" + q.Encode()
	return &ParseResult{Outbound: Outbound{"protocol": "snell", "tag": tag, "settings": settings}, Identity: identity}, nil
}
