package mtproto

import (
	"encoding/hex"
	"encoding/json"
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

func TestMigratedClientDomainsRemainUsable(t *testing.T) {
	clients := []map[string]any{}
	for _, domain := range []string{"b.example.com", "a.example.com", "b.example.com"} {
		clients = append(clients, map[string]any{
			"email": domain, "enable": true,
			"secret": "EE0123456789ABCDEF0123456789ABCDEF" + hex.EncodeToString([]byte(domain)),
		})
	}
	settings, _ := json.Marshal(map[string]any{"clients": clients})
	inst, ok := InstanceFromInbound(&model.Inbound{Protocol: model.MTProto, Settings: string(settings)})
	if !ok || inst.FakeTLSDomain != "a.example.com" || strings.Join(inst.FakeTLSDomains, ",") != "b.example.com" {
		t.Fatalf("legacy SNI migration lost domains: %+v", inst)
	}
	if !strings.Contains(renderConfig(inst, 9000, "token"), `tls_domains = ["b.example.com"]`) {
		t.Fatal("additional legacy client SNI must be accepted by Telemt")
	}
	clients[0], clients[1] = clients[1], clients[0]
	settings, _ = json.Marshal(map[string]any{"clients": clients})
	reordered, _ := InstanceFromInbound(&model.Inbound{Protocol: model.MTProto, Settings: string(settings)})
	if inst.structuralFingerprint() != reordered.structuralFingerprint() {
		t.Fatal("client reordering must not restart the sidecar")
	}
	settings, _ = json.Marshal(map[string]any{"fakeTlsDomain": "new.example.com", "clients": clients})
	changed, _ := InstanceFromInbound(&model.Inbound{Protocol: model.MTProto, Settings: string(settings)})
	if strings.Join(changed.FakeTLSDomains, ",") != "a.example.com,b.example.com" {
		t.Fatal("changing the default domain must preserve existing client links")
	}
}

func TestTelemtAutomaticMiddleProxy(t *testing.T) {
	inst := Instance{FakeTLSDomain: "example.com", Secrets: []SecretEntry{{Name: "a", Secret: "0123456789abcdef0123456789abcdef"}}}
	initialFP := inst.structuralFingerprint()
	inst.Secrets[0].AdTag = "fedcba9876543210fedcba9876543210"
	if !inst.useMiddleProxy() || initialFP == inst.structuralFingerprint() {
		t.Fatal("adding a sponsor tag must enable middle proxies and restart Telemt")
	}
	if !strings.Contains(renderConfig(inst, 9000, "token"), "use_middle_proxy = true") {
		t.Fatal("sponsor tags require Telegram middle proxies")
	}
	inst.RouteThroughXray, inst.XrayRoutePort = true, 50000
	if inst.useMiddleProxy() {
		t.Fatal("a configured local SOCKS route must take precedence")
	}
}

func TestTelemtNativeMaskSettings(t *testing.T) {
	off := false
	inst := Instance{FakeTLSDomain: "example.com", TLSMask: &off, TLSEmulation: &off, UnknownSNIAction: "reject_handshake"}
	cfg := renderConfig(inst, 9000, "token")
	for _, want := range []string{"mask = false", "tls_emulation = false", `unknown_sni_action = "reject_handshake"`, "show = []"} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing native Telemt setting: %s", want)
		}
	}
	inst.TLSMask, inst.TLSEmulation, inst.UnknownSNIAction = nil, nil, "invalid"
	if !inst.maskEnabled() || !inst.emulationEnabled() || inst.sniAction() != "mask" {
		t.Fatal("old inbounds must receive automatic camouflage defaults")
	}
}

func TestAnnounceIPRejectsUnreachableAddresses(t *testing.T) {
	for _, value := range []string{"0.0.0.0", "127.0.0.1", "224.0.0.1", "169.254.1.2", "::", "::1", "ff02::1", "fe80::1"} {
		if validAnnounceIP(value, false) != "" || validAnnounceIP(value, true) != "" {
			t.Fatalf("unreachable announcement accepted: %s", value)
		}
	}
	if validAnnounceIP("1.2.3.4", false) != "1.2.3.4" || validAnnounceIP("2001:db8::1", true) != "2001:db8::1" {
		t.Fatal("valid family-specific announcement rejected")
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

func TestLegacyFrontingMigratesToNativeMask(t *testing.T) {
	settings := `{"clients":[{"email":"alice","enable":true,"secret":"0123456789abcdef0123456789abcdef"}],"domainFronting":{"ip":"127.0.0.1","port":8443,"proxyProtocol":true}}`
	inst, ok := InstanceFromInbound(&model.Inbound{Protocol: model.MTProto, Settings: settings})
	if !ok || inst.MaskHost != "127.0.0.1" || inst.MaskPort != 8443 || inst.MaskProxyProtocol != 1 {
		t.Fatalf("legacy mask not migrated: %+v", inst)
	}
	settings = `{"clients":[{"email":"alice","enable":true,"secret":"0123456789abcdef0123456789abcdef"}],"maskHost":"","domainFronting":{"ip":"127.0.0.1","port":8443,"proxyProtocol":true}}`
	inst, _ = InstanceFromInbound(&model.Inbound{Protocol: model.MTProto, Settings: settings})
	if inst.MaskHost != "" || inst.MaskPort != 443 || inst.MaskProxyProtocol != 0 {
		t.Fatal("explicit automatic mask must override legacy fronting")
	}
	off := false
	inst.AllowLegacyModes = &off
	cfg := renderConfig(inst, 9000, "token")
	if !strings.Contains(cfg, "classic = false") || !strings.Contains(cfg, "secure = false") || !strings.Contains(cfg, "tls = true") {
		t.Fatal("new FakeTLS-only defaults not rendered")
	}
	inst.ProxyProtocolListener = true
	inst.ProxyProtocolTrustedCIDRs = []string{"10.0.0.0/8"}
	inst.MaskPort = 8443
	cfg = renderConfig(inst, 9000, "token")
	if !strings.Contains(cfg, `proxy_protocol_trusted_cidrs = ["10.0.0.0/8"]`) || !strings.Contains(cfg, "mask_port = 8443") {
		t.Fatal("native PROXY/mask options missing")
	}
}
