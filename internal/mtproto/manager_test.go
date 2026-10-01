package mtproto

import (
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestInstanceFromInboundTelemt(t *testing.T) {
	ib := &model.Inbound{
		Id: 3, Tag: "inbound-3", Listen: "0.0.0.0", Port: 8443, Protocol: model.MTProto,
		Settings: `{"fakeTlsDomain":"example.com:443","tlsDomains":["cdn.example.com","example.com"],` +
			`"telemtModes":{"classic":true,"secure":true,"tls":true},"mask":true,"tlsEmulation":true,` +
			`"proxyProtocolListener":true,"preferIp":"prefer-ipv4",` +
			`"mekoFix":{"enabled":true,"backend":"nftables","synRatePerMinute":60,"burst":2,"iosBypass":true},` +
			`"clients":[{"email":"alice@example.com","secret":"ee0123456789abcdef0123456789abcdef6578616d706c652e636f6d",` +
			`"adTag":"fedcba9876543210fedcba9876543210","enable":true,"totalGB":1073741824,"expiryTime":1893456000000,"limitIp":2,"maxTcpConns":8,"rateLimitUpBps":1000,"rateLimitDownBps":2000}]}`,
	}
	inst, ok := InstanceFromInbound(ib)
	if !ok {
		t.Fatal("expected Telemt instance")
	}
	if len(inst.Secrets) != 1 || inst.Secrets[0].Secret != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("legacy ee secret must normalize to raw Telemt secret: %+v", inst.Secrets)
	}
	if inst.TLSDomain != "example.com" || len(inst.TLSDomains) != 1 || inst.TLSDomains[0] != "cdn.example.com" {
		t.Fatalf("TLS domains not normalized: primary=%q extra=%v", inst.TLSDomain, inst.TLSDomains)
	}
	if !inst.Modes.Classic || !inst.Modes.Secure || !inst.Modes.TLS {
		t.Fatalf("Telemt modes not parsed: %+v", inst.Modes)
	}
	if !inst.MekoFix.Enabled || inst.MekoFix.Backend != "nftables" || inst.MekoFix.SynRatePerMinute != 60 || inst.MekoFix.Burst != 2 || !inst.MekoFix.IOSBypass {
		t.Fatalf("MEKO settings not parsed: %+v", inst.MekoFix)
	}
	client := inst.Secrets[0]
	if client.MaxUniqueIPs != 2 || client.MaxTCPConns != 8 || client.RateUpBps != 1000 || client.RateDownBps != 2000 {
		t.Fatalf("Telemt user limits not parsed: %+v", client)
	}
}

func TestInstanceDefaults(t *testing.T) {
	ib := &model.Inbound{Protocol: model.MTProto, Port: 443, Settings: `{"clients":[{"email":"a","secret":"0123456789abcdef0123456789abcdef","enable":true}]}`}
	inst, ok := InstanceFromInbound(ib)
	if !ok {
		t.Fatal("expected instance")
	}
	if !inst.Modes.TLS || inst.Modes.Classic || inst.Modes.Secure || !inst.Mask || !inst.TLSEmulation {
		t.Fatalf("unexpected Telemt defaults: %+v", inst)
	}
	if inst.MekoFix.Enabled || inst.MekoFix.SynRatePerMinute != 54 || inst.MekoFix.Burst != 1 || !inst.MekoFix.IOSBypass {
		t.Fatalf("existing rows must keep fix disabled but receive safe defaults: %+v", inst.MekoFix)
	}
}

func TestTelemtUsername(t *testing.T) {
	if got := telemtUsername("alice-1.test"); got != "alice-1.test" {
		t.Fatalf("safe username changed: %q", got)
	}
	a := telemtUsername("alice@example.com")
	b := telemtUsername("alice@example.com")
	if a != b || !strings.HasPrefix(a, "u_") || len(a) != 26 {
		t.Fatalf("unsafe email must map deterministically: %q / %q", a, b)
	}
}

func TestRenderTelemtConfig(t *testing.T) {
	inst := Instance{
		Listen: "0.0.0.0", Port: 443,
		Modes:     TelemtModes{Classic: true, Secure: true, TLS: true},
		TLSDomain: "example.com", TLSDomains: []string{"cdn.example.com"}, Mask: true, TLSEmulation: true,
		ProxyProtocolListener: true, PreferIP: "prefer-ipv6", RouteThroughXray: true, XrayRoutePort: 50000,
		Secrets: []SecretEntry{{Name: "alice@example.com", Secret: "0123456789abcdef0123456789abcdef", AdTag: "fedcba9876543210fedcba9876543210", QuotaBytes: 1024, ExpiresUnix: 1893456000, MaxUniqueIPs: 2, MaxTCPConns: 8, RateUpBps: 1000, RateDownBps: 2000}},
	}
	cfg := renderConfig(inst, 9091, "sesame")
	for _, want := range []string{
		"[general.modes]", "classic = true", "secure = true", "tls = true",
		"[server.api]", `listen = "127.0.0.1:9091"`, `auth_header = "Bearer sesame"`,
		"[[server.listeners]]", `ip = "0.0.0.0"`,
		"[censorship]", `tls_domain = "example.com"`, `tls_domains = ["cdn.example.com"]`, "mask = true", "tls_emulation = true",
		"[[upstreams]]", `type = "socks5"`, `address = "127.0.0.1:50000"`,
		"[access.users]", "[access.user_ad_tags]", "[access.user_data_quota]", "[access.user_max_unique_ips]", "[access.user_max_tcp_conns]", "[access.user_rate_limits.",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("Telemt config missing %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "mtg") || strings.Contains(cfg, "[secrets]") {
		t.Fatalf("Telemt config must not contain mtg sections:\n%s", cfg)
	}
}

func TestFingerprintSplit(t *testing.T) {
	base := Instance{Secrets: []SecretEntry{{Name: "a", Secret: "0123456789abcdef0123456789abcdef"}}, Listen: "0.0.0.0", Port: 443, Modes: TelemtModes{TLS: true}, TLSDomain: "example.com", Mask: true, TLSEmulation: true}
	changed := base
	changed.Secrets = []SecretEntry{{Name: "a", Secret: "fedcba9876543210fedcba9876543210"}}
	if base.structuralFingerprint() != changed.structuralFingerprint() || base.secretsFingerprint() == changed.secretsFingerprint() {
		t.Fatal("user-only changes must stay reloadable")
	}
	structural := base
	structural.TLSDomain = "example.org"
	if base.structuralFingerprint() == structural.structuralFingerprint() {
		t.Fatal("Telemt structural settings must force restart")
	}
}
