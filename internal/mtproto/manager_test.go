package mtproto

import (
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestDecodeLegacySecret(t *testing.T) {
	raw := "0123456789abcdef0123456789abcdef"
	domainHex := "6578616d706c652e636f6d"
	got, domain := decodeLegacySecret("ee" + raw + domainHex)
	if got != raw || domain != "example.com" {
		t.Fatalf("ee migration failed: raw=%q domain=%q", got, domain)
	}
	got, domain = decodeLegacySecret("dd" + raw)
	if got != raw || domain != "" {
		t.Fatalf("dd migration failed: raw=%q domain=%q", got, domain)
	}
	got, domain = decodeLegacySecret(raw)
	if got != raw || domain != "" {
		t.Fatalf("raw secret migration failed: raw=%q domain=%q", got, domain)
	}
}

func TestValidRawSecret(t *testing.T) {
	if !validRawSecret("0123456789abcdef0123456789abcdef") {
		t.Fatal("valid 32-hex secret rejected")
	}
	for _, secret := range []string{"", "short", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", "0123456789abcdef0123456789abcde"} {
		if validRawSecret(secret) {
			t.Fatalf("invalid secret accepted: %q", secret)
		}
	}
}

func TestInstanceFromInbound(t *testing.T) {
	raw := "0123456789abcdef0123456789abcdef"
	ib := &model.Inbound{
		Id: 3, Tag: "inbound-3", Listen: "0.0.0.0", Port: 8443, Protocol: model.MTProto,
		Settings: `{"fakeTlsDomain":"","routeThroughXray":true,"routeXrayPort":50000,"clients":[` +
			`{"email":"alice","secret":"ee` + raw + `6578616d706c652e636f6d","adTag":"fedcba9876543210fedcba9876543210","enable":true,"totalGB":1073741824,"expiryTime":1893456000000},` +
			`{"email":"disabled","secret":"dd` + raw + `","enable":false}]}`,
	}
	inst, ok := InstanceFromInbound(ib)
	if !ok {
		t.Fatal("expected usable Telemt instance")
	}
	if len(inst.Secrets) != 1 || inst.Secrets[0].Name != "alice" || inst.Secrets[0].Secret != raw {
		t.Fatalf("bad users: %+v", inst.Secrets)
	}
	if inst.FakeTLSDomain != "example.com" {
		t.Fatalf("expected embedded FakeTLS domain, got %q", inst.FakeTLSDomain)
	}
	if inst.Secrets[0].QuotaBytes != 1073741824 || inst.Secrets[0].ExpiresUnix != 1893456000 {
		t.Fatalf("limits not migrated: %+v", inst.Secrets[0])
	}
	if !inst.RouteThroughXray || inst.XrayRoutePort != 50000 {
		t.Fatalf("xray route lost: %+v", inst)
	}
}

func TestInstanceFromInboundRejectsInvalidSecret(t *testing.T) {
	ib := &model.Inbound{Id: 4, Protocol: model.MTProto, Settings: `{"clients":[{"email":"broken","secret":"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz","enable":true}]}`}
	if _, ok := InstanceFromInbound(ib); ok {
		t.Fatal("invalid non-hex secret must not create a Telemt instance")
	}
}

func TestRenderTelemtConfig(t *testing.T) {
	cfg := renderConfig(Instance{
		Listen: "0.0.0.0", Port: 443, FakeTLSDomain: "www.microsoft.com",
		RouteThroughXray: true, XrayRoutePort: 50000, ThrottleMaxConnections: 8,
		Secrets: []SecretEntry{
			{Name: "alice", Secret: "0123456789abcdef0123456789abcdef", AdTag: "fedcba9876543210fedcba9876543210", QuotaBytes: 1073741824, ExpiresUnix: 1893456000},
			{Name: "bob", Secret: "abcdef0123456789abcdef0123456789"},
		},
	}, 9099, "sesame")
	for _, want := range []string{
		"[general.modes]", "tls = true", "[server]", "port = 443", "[[server.listeners]]", `ip = "0.0.0.0"`,
		"[server.api]", `listen = "127.0.0.1:9099"`, `auth_header = "Bearer sesame"`,
		"[censorship]", `tls_domain = "www.microsoft.com"`, "mask = true", "tls_emulation = true",
		"[access.users]", `"alice" = "0123456789abcdef0123456789abcdef"`,
		"[access.user_ad_tags]", `"alice" = "fedcba9876543210fedcba9876543210"`,
		"[access.user_data_quota]", `"alice" = 1073741824`,
		"[access.user_expirations]", `"alice" = "2030-01-01T00:00:00Z"`,
		"user_max_tcp_conns_global_each = 8", "[[upstreams]]", `type = "socks5"`, `address = "127.0.0.1:50000"`,
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("Telemt config missing %q:\n%s", want, cfg)
		}
	}
	for _, old := range []string{"bind-to =", "api-bind-to", "[secrets]", "[secret-limits", "[domain-fronting]"} {
		if strings.Contains(cfg, old) {
			t.Fatalf("old mtg syntax leaked (%q):\n%s", old, cfg)
		}
	}
}

func TestRenderTelemtDirectUpstream(t *testing.T) {
	cfg := renderConfig(Instance{Listen: "127.0.0.1", Port: 8443, FakeTLSDomain: "example.com", Secrets: []SecretEntry{{Name: "a", Secret: "0123456789abcdef0123456789abcdef"}}}, 9000, "token")
	if !strings.Contains(cfg, `type = "direct"`) {
		t.Fatalf("expected direct upstream:\n%s", cfg)
	}
	if strings.Contains(cfg, `type = "socks5"`) {
		t.Fatalf("unexpected socks upstream:\n%s", cfg)
	}
}

func TestFingerprintSplit(t *testing.T) {
	base := Instance{Secrets: []SecretEntry{{Name: "a", Secret: "0123456789abcdef0123456789abcdef"}}, Listen: "0.0.0.0", Port: 443, FakeTLSDomain: "example.com"}
	changed := base
	changed.FakeTLSDomain = "example.org"
	if base.structuralFingerprint() == changed.structuralFingerprint() {
		t.Fatal("TLS domain must be structural")
	}
	rekey := base
	rekey.Secrets = []SecretEntry{{Name: "a", Secret: "abcdef0123456789abcdef0123456789"}}
	if base.secretsFingerprint() == rekey.secretsFingerprint() {
		t.Fatal("secret change must alter user fingerprint")
	}
	forward := Instance{Secrets: []SecretEntry{{Name: "alice", Secret: "aa"}, {Name: "bob", Secret: "bb"}}}
	reverse := Instance{Secrets: []SecretEntry{{Name: "bob", Secret: "bb"}, {Name: "alice", Secret: "aa"}}}
	if forward.secretsFingerprint() != reverse.secretsFingerprint() {
		t.Fatal("client order must not alter fingerprint")
	}

	legacy := base
	legacy.FrontingIP = "203.0.113.1"
	legacy.FrontingPort = 443
	legacy.FrontingProxyProtocol = true
	if base.structuralFingerprint() != legacy.structuralFingerprint() {
		t.Fatal("compatibility-only domainFronting fields must not restart Telemt")
	}
}

func TestMonotonicCounterDelta(t *testing.T) {
	if got := monotonicCounterDelta(120, 100); got != 20 {
		t.Fatalf("delta=%d", got)
	}
	if got := monotonicCounterDelta(5, 100); got != 5 {
		t.Fatalf("reset delta=%d", got)
	}
	if got := monotonicCounterDelta(0, 100); got != 0 {
		t.Fatalf("zero delta=%d", got)
	}
}
