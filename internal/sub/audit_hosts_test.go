package sub

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	wgutil "github.com/SawaMEN/3x-ui/v3/internal/util/wireguard"
)

func TestAuditHostStreamIsolation(t *testing.T) {
	base := map[string]any{
		"security":        "reality",
		"realitySettings": map[string]any{"serverName": "base.sni"},
		"wsSettings":      map[string]any{"host": "base.host", "path": "/base"},
	}
	first := cloneStreamForExternalProxy(base)
	applyHostStreamOverrides(map[string]any{"isHost": true, "sni": "first.sni", "hostHeader": "first.host", "path": "/first"}, first)
	second := cloneStreamForExternalProxy(base)
	if second["realitySettings"].(map[string]any)["serverName"] != "base.sni" ||
		second["wsSettings"].(map[string]any)["path"] != "/base" ||
		second["wsSettings"].(map[string]any)["host"] != "base.host" {
		t.Fatalf("endpoint override mutated the base: %#v", second)
	}
}

func TestAuditHostBlankSNI(t *testing.T) {
	ep := hostToExternalProxyMap(&model.Host{KeepSniBlank: true, Sni: "ignored"}, "example.org", 443)
	params := map[string]string{"sni": "inherited"}
	applyExternalProxyTLSParams(ep, params, "tls")
	if params["sni"] != "" {
		t.Fatalf("inherited SNI survived: %v", params)
	}
	stream := map[string]any{"security": "reality", "realitySettings": map[string]any{"serverName": "inherited"}}
	applyHostStreamOverrides(ep, stream)
	if stream["realitySettings"].(map[string]any)["serverName"] != "" {
		t.Fatal("Reality SNI was not cleared")
	}
}

func TestAuditExcludedHostDoesNotExposeDefaultEndpoint(t *testing.T) {
	seedSubDB(t)
	ib := seedSubInbound(t, "audit", "excluded", 4443, 1, wsTLSStream)
	seedHost(t, &model.Host{InboundId: ib.Id, Address: "excluded.example", ExcludeFromSubTypes: []string{"raw", "json", "clash"}})
	links, _, _, _, err := NewSubService("").GetSubs("audit", "request.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range links {
		if strings.Contains(link, "203.0.113.5") || strings.Contains(link, "excluded.example") {
			t.Fatalf("excluded endpoint leaked: %s", link)
		}
	}
	out, _, err := NewSubJsonService("", "", "", "", NewSubService("")).GetJson("audit", "request.example", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "203.0.113.5") || strings.Contains(out, "excluded.example") {
		t.Fatalf("excluded JSON endpoint leaked: %s", out)
	}
	out, _, err = NewSubClashService(false, "", NewSubService("")).GetClash("audit", "request.example")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "203.0.113.5") || strings.Contains(out, "excluded.example") {
		t.Fatalf("excluded Clash endpoint leaked: %s", out)
	}
}

func TestAuditHostShuffleKeepsOtherGroupsAndCache(t *testing.T) {
	hosts := []*model.Host{{Id: 1}, {Id: 2, GroupId: "shuffle", ShuffleHost: true}, {Id: 3}, {Id: 4, GroupId: "shuffle", ShuffleHost: true}}
	changed := false
	for range 64 {
		out := shuffledHosts(hosts)
		if out[0] != hosts[0] || out[2] != hosts[2] || hosts[1].Id != 2 || hosts[3].Id != 4 {
			t.Fatal("shuffle changed another group or the cached order")
		}
		changed = changed || out[1].Id == 4
	}
	if !changed {
		t.Fatal("opted-in group never shuffled")
	}
}

func TestAuditNativeExcludedProtocolDoesNotTriggerFallback(t *testing.T) {
	seedSubDB(t)
	ib := seedSubInbound(t, "audit-native", "excluded", 4443, 1, wsTLSStream)
	serverKey, _, err := wgutil.GenerateWireguardKeypair()
	if err != nil {
		t.Fatal(err)
	}
	clientKey, _, err := wgutil.GenerateWireguardKeypair()
	if err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf(`{"secretKey":%q,"clients":[{"email":"excluded@e","subId":"audit-native","enable":true,"privateKey":%q,"allowedIPs":["10.0.0.2/32"]}]}`, serverKey, clientKey)
	if err := database.GetDB().Model(ib).Updates(map[string]any{"protocol": model.WireGuard, "settings": settings}).Error; err != nil {
		t.Fatal(err)
	}
	seedHost(t, &model.Host{InboundId: ib.Id, Address: "excluded.example", ExcludeFromSubTypes: []string{"json"}})
	out, _, err := NewSubJsonService("", "", "", "", NewSubService("")).GetSingBoxJson("audit-native", "request.example", false)
	if err != nil || out != "" {
		t.Fatalf("excluded WireGuard must not cause fallback: %q, %v", out, err)
	}
}

func TestAuditClashExcludedUnsupportedProtocol(t *testing.T) {
	seedSubDB(t)
	seedSubInbound(t, "audit-clash", "included", 4443, 1, wsTLSStream)
	ib := seedSubInbound(t, "audit-clash", "excluded", 4444, 2, wsTLSStream)
	if err := database.GetDB().Model(ib).Update("protocol", model.TrustTunnel).Error; err != nil {
		t.Fatal(err)
	}
	seedHost(t, &model.Host{InboundId: ib.Id, Address: "excluded.example", ExcludeFromSubTypes: []string{"clash"}})
	out, _, err := NewSubClashService(false, "", NewSubService("")).GetClash("audit-clash", "request.example")
	if err != nil || !strings.Contains(out, "4443") || strings.Contains(out, "excluded.example") {
		t.Fatalf("excluded unsupported inbound affected Clash: %s, %v", out, err)
	}
}

func TestAuditNativeNaiveBlankSNI(t *testing.T) {
	inbound := &model.Inbound{Listen: "origin.example", Port: 443, Protocol: model.NaiveProxy,
		Settings: `{"network":"tcp","tls":{"serverName":"inherited.example"}}`}
	out := NewSubJsonService("", "", "", "", nil).genNativeNaive(&SubService{address: "sub.example"}, inbound,
		model.Client{Email: "client", Password: "secret"}, map[string]any{"dest": "edge.example", "keepSniBlank": true})
	tls, _ := out["tls"].(map[string]any)
	if _, exists := tls["server_name"]; exists {
		t.Fatalf("blank SNI inherited a hostname: %#v", tls)
	}
}
