package singbox

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestTranslateXrayVLESSWebSocketTLS(t *testing.T) {
	raw := map[string]any{
		"protocol": "vless",
		"tag":      "vless-443",
		"listen":   "0.0.0.0",
		"port":     443,
		"settings": map[string]any{
			"clients": []any{
				map[string]any{
					"id":    "11111111-1111-1111-1111-111111111111",
					"email": "alice",
				},
			},
		},
		"streamSettings": map[string]any{
			"network":  "ws",
			"security": "tls",
			"tlsSettings": map[string]any{
				"serverName": "example.com",
			},
			"wsSettings": map[string]any{
				"path": "/ws",
			},
		},
	}

	got, err := TranslateXrayInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "vless" || got["listen_port"] != 443 {
		t.Fatalf("unexpected base config: %#v", got)
	}
	users, ok := got["users"].([]map[string]any)
	if !ok || len(users) != 1 || users[0]["uuid"] != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected users: %#v", got["users"])
	}
	tls, ok := got["tls"].(map[string]any)
	if !ok || tls["enabled"] != true || tls["server_name"] != "example.com" {
		t.Fatalf("unexpected tls: %#v", got["tls"])
	}
	transport, ok := got["transport"].(map[string]any)
	if !ok || transport["type"] != "ws" || transport["path"] != "/ws" {
		t.Fatalf("unexpected transport: %#v", got["transport"])
	}
}

func TestRewriteDNSOutboundRoutes(t *testing.T) {
	route := map[string]any{
		"final": "dns",
		"rules": []map[string]any{
			{"domain": []string{"example.com"}, "action": "route", "outbound": "direct"},
			{"protocol": []string{"dns"}, "action": "route", "outbound": "dns"},
		},
	}
	RewriteDNSOutboundRoutes(route, map[string]struct{}{"dns": {}})
	if route["final"] != "direct" {
		t.Fatalf("unexpected DNS final target: %#v", route["final"])
	}
	rules := route["rules"].([]map[string]any)
	if len(rules) != 3 || rules[0]["outbound"] != "direct" || rules[1]["action"] != "hijack-dns" || rules[2]["action"] != "hijack-dns" {
		t.Fatalf("unexpected DNS route rules: %#v", rules)
	}
	if _, ok := rules[1]["outbound"]; ok {
		t.Fatalf("DNS outbound target was not removed: %#v", rules[1])
	}
}

func TestTranslateXrayDomainStrategy(t *testing.T) {
	cases := map[string]string{
		"UseIPv4":   "prefer_ipv4",
		"UseIPv4v6": "prefer_ipv4",
		"UseIPv6":   "prefer_ipv6",
		"UseIPv6v4": "prefer_ipv6",
		"ForceIPv4": "ipv4_only",
		"ForceIPv6": "ipv6_only",
		"AsIs":      "",
		"ForceIP":   "",
	}
	for input, want := range cases {
		if got := TranslateXrayDomainStrategy(input); got != want {
			t.Fatalf("%s => %q, want %q", input, got, want)
		}
	}
}

func TestTranslateXrayWireGuardToEndpoint(t *testing.T) {
	raw := map[string]any{
		"protocol": "wireguard",
		"tag":      "warp",
		"settings": map[string]any{
			"secretKey": "private-key",
			"address":   []any{"10.0.0.2", "fd00::2/128"},
			"mtu":       1420,
			"reserved":  []any{1.0, 2.0, 3.0},
			"peers": []any{
				map[string]any{
					"publicKey": "public-key",
					"psk":       "psk",
					"allowedIPs": []any{
						"0.0.0.0/0",
						"::/0",
					},
					"endpoint":  "example.com:51820",
					"keepAlive": 25,
				},
			},
		},
	}
	got, err := TranslateXrayWireGuardEndpoint(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "wireguard" || got["tag"] != "warp" {
		t.Fatalf("unexpected endpoint: %#v", got)
	}
	if system, _ := got["system"].(bool); system {
		t.Fatalf("translated WireGuard outbound must remain in userspace mode: %#v", got)
	}
	if got["domain_resolver"] != nil {
		t.Fatalf("domain peer must inherit the configured route resolver: %#v", got)
	}
	addresses, ok := got["address"].([]string)
	if !ok || len(addresses) != 2 || addresses[0] != "10.0.0.2/32" {
		t.Fatalf("unexpected endpoint addresses: %#v", got["address"])
	}
	peers, ok := got["peers"].([]map[string]any)
	if !ok || len(peers) != 1 {
		t.Fatalf("unexpected endpoint peers: %#v", got["peers"])
	}
	if peers[0]["address"] != "example.com" || peers[0]["port"] != 51820 ||
		peers[0]["public_key"] != "public-key" || peers[0]["pre_shared_key"] != "psk" ||
		peers[0]["persistent_keepalive_interval"] != 25 {
		t.Fatalf("unexpected endpoint peer: %#v", peers[0])
	}
}

func TestTranslateXrayVLESSRawTransportUsesPlainTCP(t *testing.T) {
	raw := map[string]any{
		"protocol": "vless",
		"tag":      "vless-raw",
		"settings": map[string]any{
			"clients": []any{
				map[string]any{
					"id":    "11111111-1111-1111-1111-111111111111",
					"email": "alice",
				},
			},
		},
		"streamSettings": map[string]any{
			"network": "raw",
		},
	}
	got, err := TranslateXrayInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["transport"]; ok {
		t.Fatalf("raw TCP must not emit a sing-box transport: %#v", got["transport"])
	}
}

func TestTranslateXrayVLESSHTTPTransport(t *testing.T) {
	raw := map[string]any{
		"protocol": "vless",
		"tag":      "vless-http",
		"settings": map[string]any{
			"clients": []any{
				map[string]any{
					"id":    "11111111-1111-1111-1111-111111111111",
					"email": "alice",
				},
			},
		},
		"streamSettings": map[string]any{
			"network": "http",
			"httpSettings": map[string]any{
				"host":   []any{"example.com", "www.example.com"},
				"path":   "/api",
				"method": "GET",
				"headers": map[string]any{
					"X-Forwarded-Proto": "https",
				},
			},
		},
	}
	got, err := TranslateXrayInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := got["transport"].(map[string]any)
	if !ok || transport["type"] != "http" || transport["path"] != "/api" || transport["method"] != "GET" {
		t.Fatalf("unexpected HTTP transport: %#v", got["transport"])
	}
	hosts, ok := transport["host"].([]string)
	if !ok || len(hosts) != 2 || hosts[0] != "example.com" || hosts[1] != "www.example.com" {
		t.Fatalf("unexpected HTTP hosts: %#v", transport["host"])
	}
}

func TestTranslateXrayHysteria2Inbound(t *testing.T) {
	raw := map[string]any{
		"protocol": "hysteria",
		"tag":      "in-53547-udp",
		"listen":   "0.0.0.0",
		"port":     53547,
		"settings": map[string]any{
			"clients": []any{
				map[string]any{
					"email": "hy2-user",
					"auth":  "secret-auth",
				},
			},
		},
		"streamSettings": map[string]any{
			"network":  "hysteria",
			"security": "tls",
			"hysteriaSettings": map[string]any{
				"version":        2,
				"udpIdleTimeout": 60,
			},
			"tlsSettings": map[string]any{
				"serverName": "example.com",
			},
		},
	}

	got, err := TranslateXrayInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "hysteria2" || got["listen_port"] != 53547 {
		t.Fatalf("unexpected Hysteria2 base config: %#v", got)
	}
	users, ok := got["users"].([]map[string]any)
	if !ok || len(users) != 1 || users[0]["password"] != "secret-auth" {
		t.Fatalf("unexpected Hysteria2 users: %#v", got["users"])
	}
	if _, ok := got["idle_timeout"]; ok {
		t.Fatalf("udpIdleTimeout must not be mapped to QUIC idle_timeout: %#v", got["idle_timeout"])
	}
	tls, ok := got["tls"].(map[string]any)
	if !ok || tls["enabled"] != true || tls["server_name"] != "example.com" {
		t.Fatalf("unexpected Hysteria2 TLS: %#v", got["tls"])
	}
}

func TestTranslateXrayInboundRejectsRealityUntilMapped(t *testing.T) {
	raw := map[string]any{
		"protocol": "vless",
		"tag":      "reality",
		"port":     443,
		"streamSettings": map[string]any{
			"network":  "tcp",
			"security": "reality",
		},
	}
	if _, err := TranslateXrayInbound(raw); err == nil {
		t.Fatal("expected REALITY compatibility guard")
	}
}

func TestTranslateXrayInboundRejectsUnknownTransport(t *testing.T) {
	raw := map[string]any{
		"protocol": "vless",
		"tag":      "xhttp",
		"port":     443,
		"streamSettings": map[string]any{
			"network": "xhttp",
		},
	}
	if _, err := TranslateXrayInbound(raw); err == nil {
		t.Fatal("expected unsupported transport error")
	}
}

func TestTranslateXrayRoutingSupportsBalancerTag(t *testing.T) {
	raw := map[string]any{
		"domainStrategy": "AsIs",
		"rules": []any{
			map[string]any{
				"domain":      []any{"example.com"},
				"balancerTag": "proxy-pool",
				"type":        "field",
			},
		},
		"balancers": []any{
			map[string]any{
				"tag":      "proxy-pool",
				"selector": []any{"proxy-a"},
			},
		},
	}
	route, err := TranslateXrayRouting(raw)
	if err != nil {
		t.Fatal(err)
	}
	rules, ok := route["rules"].([]map[string]any)
	if !ok || len(rules) != 1 || rules[0]["outbound"] != "proxy-pool" {
		t.Fatalf("unexpected balancer route: %#v", route["rules"])
	}
	balancers, err := TranslateXrayBalancers(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(balancers) != 1 || balancers[0]["type"] != "selector" || balancers[0]["tag"] != "proxy-pool" {
		t.Fatalf("unexpected balancer outbound: %#v", balancers)
	}
}

func TestTranslateXrayBalancersRejectsDynamicPool(t *testing.T) {
	raw := map[string]any{"balancers": []any{map[string]any{"tag": "pool", "selector": []any{"proxy-a", "proxy-b"}}}}
	if _, err := TranslateXrayBalancers(raw); err == nil {
		t.Fatal("dynamic pool was silently changed to a static selector")
	}
}

func TestTranslateXrayRoutingSkipsInternalAPIRule(t *testing.T) {
	raw := map[string]any{
		"domainStrategy": "AsIs",
		"rules": []any{
			map[string]any{
				"inboundTag":  []any{"api"},
				"outboundTag": "api",
				"type":        "field",
			},
			map[string]any{
				"ip":          []any{"geoip:private"},
				"outboundTag": "blocked",
				"type":        "field",
			},
		},
	}
	got, err := TranslateXrayRouting(raw)
	if err != nil {
		t.Fatal(err)
	}
	rules, ok := got["rules"].([]map[string]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("expected one real routing rule, got %#v", got["rules"])
	}
	if rules[0]["outbound"] != "blocked" {
		t.Fatalf("unexpected translated rule: %#v", rules[0])
	}
}

func TestTranslateXrayShadowsocksInboundMapsMethod(t *testing.T) {
	raw := map[string]any{
		"protocol": "shadowsocks",
		"tag":      "ss-443",
		"port":     443,
		"settings": map[string]any{
			"method": "2022-blake3-aes-128-gcm",
			"clients": []any{
				map[string]any{
					"email":    "alice",
					"password": "test-key",
				},
			},
		},
	}
	got, err := TranslateXrayInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "shadowsocks" || got["method"] != "2022-blake3-aes-128-gcm" {
		t.Fatalf("unexpected shadowsocks config: %#v", got)
	}
	users, ok := got["users"].([]map[string]any)
	if !ok || len(users) != 1 || users[0]["name"] != "alice" || users[0]["password"] != "test-key" {
		t.Fatalf("unexpected shadowsocks users: %#v", got["users"])
	}
}

func TestTranslateXrayInboundRejectsWireGuard(t *testing.T) {
	raw := map[string]any{"protocol": "wireguard", "tag": "wg-1", "port": 51820}
	if _, err := TranslateXrayInbound(raw); err == nil {
		t.Fatal("expected WireGuard compatibility error")
	}
}

func TestTranslateXrayVLESSOutboundMapsServer(t *testing.T) {
	raw := map[string]any{
		"protocol": "vless",
		"tag":      "proxy",
		"settings": map[string]any{
			"vnext": []any{
				map[string]any{
					"address": "example.com",
					"port":    443,
					"users": []any{
						map[string]any{
							"id":         "11111111-1111-1111-1111-111111111111",
							"encryption": "none",
						},
					},
				},
			},
		},
	}
	got, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "vless" || got["server"] != "example.com" || got["server_port"] != 443 {
		t.Fatalf("unexpected VLESS outbound: %#v", got)
	}
	if got["uuid"] != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected VLESS uuid: %#v", got["uuid"])
	}
}

func TestTranslateXrayHysteria2InboundKeepsMasqueradeWhenUsersExist(t *testing.T) {
	raw := map[string]any{
		"protocol": "hysteria",
		"tag":      "hy2-in",
		"settings": map[string]any{
			"clients": []any{
				map[string]any{"email": "alice", "auth": "secret"},
			},
		},
		"streamSettings": map[string]any{
			"network":  "hysteria",
			"security": "tls",
			"tlsSettings": map[string]any{
				"serverName": "example.com",
			},
			"hysteriaSettings": map[string]any{
				"version": 2,
				"masquerade": map[string]any{
					"type":    "string",
					"content": "hello",
				},
			},
		},
	}
	got, err := TranslateXrayInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "hysteria2" {
		t.Fatalf("unexpected Hysteria2 type: %#v", got["type"])
	}
	masquerade, ok := got["masquerade"].(map[string]any)
	if !ok || masquerade["type"] != "string" || masquerade["content"] != "hello" {
		t.Fatalf("masquerade must be preserved when users are configured: %#v", got["masquerade"])
	}
}

func TestTranslatePanelVLESSFlatOutbound(t *testing.T) {
	raw := map[string]any{
		"protocol": "vless",
		"tag":      "vless-flat",
		"settings": map[string]any{
			"address":    "example.com",
			"port":       443,
			"id":         "11111111-1111-1111-1111-111111111111",
			"encryption": "none",
			"flow":       "xtls-rprx-vision",
		},
	}
	got, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["server"] != "example.com" || got["server_port"] != 443 ||
		got["uuid"] != "11111111-1111-1111-1111-111111111111" ||
		got["flow"] != "xtls-rprx-vision" {
		t.Fatalf("unexpected flat VLESS outbound: %#v", got)
	}
}

func TestTranslatePanelHysteriaFlatOutbound(t *testing.T) {
	raw := map[string]any{
		"protocol": "hysteria",
		"tag":      "hy2-flat",
		"settings": map[string]any{
			"address": "example.com",
			"port":    443,
			"version": 2,
		},
		"streamSettings": map[string]any{
			"network": "hysteria",
			"hysteriaSettings": map[string]any{
				"version": 2,
				"auth":    "secret",
			},
		},
	}
	got, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "hysteria2" || got["server"] != "example.com" || got["server_port"] != 443 ||
		got["password"] != "secret" {
		t.Fatalf("unexpected flat Hysteria2 outbound: %#v", got)
	}
	tls, ok := got["tls"].(map[string]any)
	if !ok || tls["enabled"] != true {
		t.Fatalf("flat Hysteria2 outbound must synthesize TLS: %#v", got["tls"])
	}
}

func TestTranslateXrayHysteria2OutboundMapsTypeAndPassword(t *testing.T) {
	raw := map[string]any{
		"protocol": "hysteria",
		"tag":      "hy2-out",
		"settings": map[string]any{
			"servers": []any{
				map[string]any{
					"address": "example.com",
					"port":    443,
				},
			},
		},
		"streamSettings": map[string]any{
			"network":  "hysteria",
			"security": "tls",
			"hysteriaSettings": map[string]any{
				"version": 2,
				"auth":    "secret",
			},
			"tlsSettings": map[string]any{
				"serverName": "example.com",
			},
		},
	}
	got, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "hysteria2" || got["password"] != "secret" {
		t.Fatalf("unexpected Hysteria2 outbound: %#v", got)
	}
}

func TestTranslateXrayRealityOutboundUsesClientFields(t *testing.T) {
	raw := map[string]any{
		"protocol": "vless",
		"tag":      "reality-out",
		"settings": map[string]any{
			"vnext": []any{
				map[string]any{
					"address": "example.com",
					"port":    443,
					"users": []any{
						map[string]any{
							"id":         "11111111-1111-1111-1111-111111111111",
							"encryption": "none",
						},
					},
				},
			},
		},
		"streamSettings": map[string]any{
			"network":  "tcp",
			"security": "reality",
			"tlsSettings": map[string]any{
				"serverName": "example.com",
			},
			"realitySettings": map[string]any{
				"publicKey": "public-key",
				"shortId":   "01234567",
			},
		},
	}
	got, err := TranslateXrayOutbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	tls, ok := got["tls"].(map[string]any)
	if !ok {
		t.Fatalf("missing tls: %#v", got)
	}
	reality, ok := tls["reality"].(map[string]any)
	if !ok || reality["public_key"] != "public-key" || reality["short_id"] != "01234567" {
		t.Fatalf("unexpected outbound Reality: %#v", tls["reality"])
	}
	if _, exists := reality["handshake"]; exists {
		t.Fatal("outbound Reality must not contain a server handshake")
	}
}

func TestTranslateXrayNaiveInbound(t *testing.T) {
	raw := map[string]any{
		"protocol": "naive",
		"tag":      "naive-443",
		"port":     443,
		"settings": map[string]any{
			"network": "tcp",
			"clients": []any{
				map[string]any{
					"email":    "alice@example.com",
					"password": "secret",
				},
			},
			"tls": map[string]any{
				"serverName":      "example.com",
				"certificatePath": "/etc/3x-ui/fullchain.pem",
				"keyPath":         "/etc/3x-ui/key.pem",
			},
		},
	}

	got, err := TranslateXrayInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "naive" ||
		rawInt(got, "listen_port") != 443 || got["network"] != "tcp" {
		t.Fatalf("unexpected Naive base config: %#v", got)
	}

	users, ok := got["users"].([]map[string]any)
	if !ok || len(users) != 1 ||
		users[0]["username"] != "alice@example.com" ||
		users[0]["password"] != "secret" {
		t.Fatalf("unexpected Naive users: %#v", got["users"])
	}

	tls, ok := got["tls"].(map[string]any)
	if !ok || tls["enabled"] != true ||
		tls["server_name"] != "example.com" ||
		tls["certificate_path"] != "/etc/3x-ui/fullchain.pem" ||
		tls["key_path"] != "/etc/3x-ui/key.pem" {
		t.Fatalf("unexpected Naive TLS: %#v", got["tls"])
	}
}

func TestTranslateXrayAnyTLSInbound(t *testing.T) {
	raw := map[string]any{
		"protocol": "anytls",
		"tag":      "anytls-443",
		"listen":   "0.0.0.0",
		"port":     443,
		"settings": map[string]any{
			"paddingScheme": []any{"stop=8", "0=30-30"},
			"tls": map[string]any{
				"serverName":      "example.com",
				"certificatePath": "/cert/fullchain.pem",
				"keyPath":         "/cert/privkey.pem",
			},
			"clients": []any{
				map[string]any{
					"email":    "alice",
					"password": "secret",
				},
			},
		},
	}
	got, err := TranslateXrayInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "anytls" || got["listen_port"] != 443 {
		t.Fatalf("unexpected AnyTLS config: %#v", got)
	}
	users, ok := got["users"].([]map[string]any)
	if !ok || len(users) != 1 || users[0]["name"] != "alice" || users[0]["password"] != "secret" {
		t.Fatalf("unexpected AnyTLS users: %#v", got["users"])
	}
	padding, ok := got["padding_scheme"].([]string)
	if !ok || len(padding) != 2 || padding[0] != "stop=8" {
		t.Fatalf("unexpected AnyTLS padding: %#v", got["padding_scheme"])
	}
	tls, ok := got["tls"].(map[string]any)
	if !ok || tls["enabled"] != true || tls["server_name"] != "example.com" ||
		tls["certificate_path"] != "/cert/fullchain.pem" || tls["key_path"] != "/cert/privkey.pem" {
		t.Fatalf("unexpected AnyTLS TLS: %#v", got["tls"])
	}
}

func TestTranslateXrayShadowTLSInbound(t *testing.T) {
	raw := map[string]any{
		"protocol": "shadowtls",
		"tag":      "shadowtls-443",
		"listen":   "0.0.0.0",
		"port":     443,
		"settings": map[string]any{
			"version":  3,
			"innerKey": "MDEyMzQ1Njc4OWFiY2RlZg==",
			"handshake": map[string]any{
				"server":     "cloudflare.com",
				"serverPort": 443,
			},
			"strictMode":  true,
			"wildcardSni": "authed",
			"handshakeForServerName": map[string]any{
				"alt.example.com": map[string]any{"server": "alt.example.com", "serverPort": 8443},
			},
			"clients": []any{
				map[string]any{
					"email":    "alice",
					"password": "secret",
				},
			},
		},
	}
	got, err := TranslateXrayInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "shadowtls" || got["version"] != 3 || got["listen_port"] != 443 || got["detour"] != "__shadowtls_ss_shadowtls-443" {
		t.Fatalf("unexpected ShadowTLS config: %#v", got)
	}
	handshake, ok := got["handshake"].(map[string]any)
	if !ok || handshake["server"] != "cloudflare.com" || handshake["server_port"] != 443 {
		t.Fatalf("unexpected ShadowTLS handshake: %#v", got["handshake"])
	}
	named, ok := got["handshake_for_server_name"].(map[string]any)
	if !ok || named["alt.example.com"].(map[string]any)["server_port"] != 8443 {
		t.Fatalf("unexpected named ShadowTLS handshake: %#v", got["handshake_for_server_name"])
	}
	if got["strict_mode"] != true || got["wildcard_sni"] != "authed" {
		t.Fatalf("unexpected ShadowTLS options: %#v", got)
	}
	users, ok := got["users"].([]map[string]any)
	if !ok || len(users) != 1 || users[0]["name"] != "alice" || users[0]["password"] != "secret" {
		t.Fatalf("unexpected ShadowTLS users: %#v", got["users"])
	}
	inner, err := TranslateShadowTLSInnerInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	innerUsers, ok := inner["users"].([]map[string]any)
	if inner["type"] != "shadowsocks" || inner["tag"] != got["detour"] || inner["listen"] != "127.0.0.1" ||
		inner["listen_port"] != 0 || inner["method"] != "2022-blake3-aes-128-gcm" ||
		inner["password"] != "MDEyMzQ1Njc4OWFiY2RlZg==" || !ok || len(innerUsers) != 1 ||
		innerUsers[0]["name"] != "alice" || innerUsers[0]["password"] != model.ShadowTLSClientKey("alice", "secret") {
		t.Fatalf("unexpected inner Shadowsocks inbound: %#v", inner)
	}
}

func TestTranslateXrayShadowTLSInboundWildcardSNIAllAllowsEmptyHandshakeServer(t *testing.T) {
	raw := map[string]any{
		"protocol": "shadowtls",
		"tag":      "shadowtls-wildcard-all",
		"port":     443,
		"settings": map[string]any{
			"version":     3,
			"handshake":   map[string]any{},
			"wildcardSni": "all",
			"clients": []any{
				map[string]any{
					"email":    "alice",
					"password": "secret",
				},
			},
		},
	}
	got, err := TranslateXrayInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["wildcard_sni"] != "all" {
		t.Fatalf("unexpected wildcard_sni: %#v", got["wildcard_sni"])
	}
	handshake, ok := got["handshake"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected ShadowTLS handshake: %#v", got["handshake"])
	}
	if _, ok := handshake["server"]; ok {
		t.Fatalf("unexpected handshake server for wildcard_sni=all: %#v", handshake)
	}
	if handshake["server_port"] != 443 {
		t.Fatalf("unexpected handshake port: %#v", handshake["server_port"])
	}
}

func TestTranslateXrayShadowTLSEmptyHandshakeUsesDefaultServer(t *testing.T) {
	raw := map[string]any{
		"protocol": "shadowtls",
		"tag":      "shadowtls-default-handshake",
		"port":     443,
		"settings": map[string]any{
			"version":     3,
			"handshake":   map[string]any{},
			"wildcardSni": "off",
		},
	}
	got, err := TranslateXrayInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	handshake, ok := got["handshake"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected ShadowTLS handshake: %#v", got["handshake"])
	}
	if handshake["server"] != "cloudflare.com" || handshake["server_port"] != 443 {
		t.Fatalf("unexpected default handshake: %#v", handshake)
	}
}
