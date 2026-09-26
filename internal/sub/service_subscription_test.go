package sub

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
	wgutil "github.com/SawaMEN/3x-ui/v3/internal/util/wireguard"
)


// Subscription traffic is indexed once per subscriber so traffic-aware
// remark templates do not fall back to one DB query per client/link.
func TestGetInboundsBySubIdIndexesTrafficByEmail(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-stats-index"
	db := database.GetDB()
	inbound := &model.Inbound{
		UserId: 1, Tag: "stats-index", Enable: true, Port: 43101, Protocol: model.VLESS,
		Settings: `{"clients":[{"id":"11111111-2222-4333-8444-555555555555","email":"stats@example.com","subId":"sub-stats-index","enable":true}]}`,
		StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	client := &model.ClientRecord{
		Email: "stats@example.com", SubID: subID,
		UUID: "11111111-2222-4333-8444-555555555555", Enable: true,
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inbound.Id}).Error; err != nil {
		t.Fatalf("attach client: %v", err)
	}
	if err := db.Create(&xray.ClientTraffic{
		InboundId: inbound.Id, Email: client.Email, Up: 1234, Down: 5678, Enable: true,
	}).Error; err != nil {
		t.Fatalf("seed traffic: %v", err)
	}

	svc := NewSubService("")
	svc.PrepareForRequest("sub.example.com")
	inbounds, err := svc.getInboundsBySubId(subID)
	if err != nil {
		t.Fatalf("getInboundsBySubId: %v", err)
	}
	if len(inbounds) != 1 {
		t.Fatalf("inbounds = %d, want 1", len(inbounds))
	}
	stats, ok := svc.statsByEmail[client.Email]
	if !ok {
		t.Fatalf("statsByEmail missing %q after subscription preload", client.Email)
	}
	if stats.Up != 1234 || stats.Down != 5678 {
		t.Fatalf("statsByEmail[%q] = %+v, want up=1234 down=5678", client.Email, stats)
	}
}


func TestGetSingBoxJsonKeepsTUIC(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-tuic-singbox"
	db := database.GetDB()
	inbound := &model.Inbound{
		UserId: 1, Tag: "tuic-singbox", Enable: true, Port: 443, Protocol: model.TUIC,
		Settings: `{"clients":[{"id":"11111111-2222-4333-8444-555555555555","email":"tuic@example.com","subId":"sub-tuic-singbox","password":"secret","enable":true}]}`,
		StreamSettings: `{"network":"tcp","security":"tls","tlsSettings":{"serverName":"tuic.example.com"}}`,
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	client := &model.ClientRecord{Email: "tuic@example.com", SubID: subID, UUID: "11111111-2222-4333-8444-555555555555", Password: "secret", Enable: true}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inbound.Id}).Error; err != nil {
		t.Fatalf("attach client: %v", err)
	}

	out, _, err := NewSubJsonService("", "", "", "", NewSubService("")).GetSingBoxJson(subID, "sub.example.com", false)
	if err != nil {
		t.Fatalf("GetSingBoxJson: %v", err)
	}
	if !strings.Contains(out, `"type": "tuic"`) {
		t.Fatalf("sing-box subscription dropped TUIC:\n%s", out)
	}
	if strings.Contains(out, `"protocol": "tuic"`) {
		t.Fatalf("Xray TUIC shape leaked into sing-box subscription:\n%s", out)
	}
}

func TestGetSingBoxJsonDoesNotCollapseMultipleWireGuardInbounds(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	serverPrivA, _, err := wgutil.GenerateWireguardKeypair()
	if err != nil { t.Fatalf("server keypair A: %v", err) }
	serverPrivB, _, err := wgutil.GenerateWireguardKeypair()
	if err != nil { t.Fatalf("server keypair B: %v", err) }
	clientPriv, _, err := wgutil.GenerateWireguardKeypair()
	if err != nil { t.Fatalf("client keypair: %v", err) }

	const subID = "sub-wg-singbox"
	db := database.GetDB()
	inboundA := &model.Inbound{UserId: 1, Tag: "wg-a", Enable: true, Listen: "0.0.0.0", Port: 51820, Protocol: model.WireGuard, Settings: `{"secretKey":"` + serverPrivA + `"}`}
	inboundB := &model.Inbound{UserId: 1, Tag: "wg-b", Enable: true, Listen: "0.0.0.0", Port: 51821, Protocol: model.WireGuard, Settings: `{"secretKey":"` + serverPrivB + `"}`}
	if err := db.Create(inboundA).Error; err != nil { t.Fatalf("seed inbound A: %v", err) }
	if err := db.Create(inboundB).Error; err != nil { t.Fatalf("seed inbound B: %v", err) }
	client := &model.ClientRecord{Email: "wg@example.com", SubID: subID, UUID: "11111111-2222-4333-8444-555555555555", PrivateKey: clientPriv, AllowedIPs: "10.0.0.2/32", Enable: true}
	if err := db.Create(client).Error; err != nil { t.Fatalf("seed client: %v", err) }
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inboundA.Id}).Error; err != nil { t.Fatalf("attach A: %v", err) }
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inboundB.Id}).Error; err != nil { t.Fatalf("attach B: %v", err) }

	out, _, err := NewSubJsonService("", "", "", "", NewSubService("")).GetSingBoxJson(subID, "wg.example.com", false)
	if err != nil { t.Fatalf("GetSingBoxJson: %v", err) }
	if got := strings.Count(out, `"type": "wireguard"`); got < 2 {
		t.Fatalf("sing-box subscription collapsed WireGuard inbounds: found %d wireguard outbounds\n%s", got, out)
	}
}
func TestGetSubsIncludesAnyTLSAndShadowTLS(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-anytls-shadowtls"
	db := database.GetDB()

	anytls := &model.Inbound{
		UserId: 1, Tag: "anytls", Enable: true, Listen: "anytls.example.com", Port: 8443, Protocol: model.AnyTLS,
		Settings: fmt.Sprintf(`{"tls":{"serverName":"anytls.example.com"},"clients":[{"email":"anytls@example.com","password":"anytls-pass","subId":%q,"enable":true}]}`, subID),
		StreamSettings: `{}`,
	}
	shadowtls := &model.Inbound{
		UserId: 1, Tag: "shadowtls", Enable: true, Listen: "shadowtls.example.com", Port: 9443, Protocol: model.ShadowTLS,
		Settings: fmt.Sprintf(`{"version":3,"handshake":{},"clients":[{"email":"shadowtls@example.com","password":"shadow-pass","subId":%q,"enable":true}]}`, subID),
		StreamSettings: `{}`,
	}
	for _, inbound := range []*model.Inbound{anytls, shadowtls} {
		if err := db.Create(inbound).Error; err != nil {
			t.Fatalf("seed %s: %v", inbound.Protocol, err)
		}
	}

	// Simulate legacy normalized rows: the protocol password is present in
	// inbound settings but missing from clients.password.
	anyClient := &model.ClientRecord{Email: "anytls@example.com", SubID: subID, Enable: true}
	shadowClient := &model.ClientRecord{Email: "shadowtls@example.com", SubID: subID, Enable: true}
	for _, client := range []*model.ClientRecord{anyClient, shadowClient} {
		if err := db.Create(client).Error; err != nil {
			t.Fatalf("seed client %s: %v", client.Email, err)
		}
	}
	if err := db.Create(&model.ClientInbound{ClientId: anyClient.Id, InboundId: anytls.Id}).Error; err != nil {
		t.Fatalf("attach AnyTLS: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: shadowClient.Id, InboundId: shadowtls.Id}).Error; err != nil {
		t.Fatalf("attach ShadowTLS: %v", err)
	}

	links, _, _, _, err := NewSubService("").GetSubs(subID, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}
	joined := strings.Join(links, "\n")
	if !strings.Contains(joined, "anytls://anytls-pass@anytls.example.com:8443") {
		t.Fatalf("raw subscription is missing AnyTLS: %v", links)
	}
	if !strings.Contains(joined, "shadowtls://shadow-pass@shadowtls.example.com:9443") {
		t.Fatalf("raw subscription is missing ShadowTLS: %v", links)
	}
	if !strings.Contains(joined, "sni=cloudflare.com") {
		t.Fatalf("ShadowTLS subscription link is missing handshake SNI: %v", links)
	}
	jsonSub, _, err := NewSubJsonService("", "", "", "", NewSubService("")).GetSingBoxJson(subID, "sub.example.com", false)
	if err != nil || !strings.Contains(jsonSub, `"type": "shadowtls"`) || !strings.Contains(jsonSub, `"server_name": "cloudflare.com"`) {
		t.Fatalf("sing-box subscription is missing ShadowTLS default handshake: %v\n%s", err, jsonSub)
	}
}
func TestGetSubsSkipsEmptyRenderedLinksButKeepsTraffic(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-empty-link"
	db := database.GetDB()
	inbound := &model.Inbound{
		UserId: 1, Tag: "naive-empty", Enable: true, Port: 8443, Protocol: model.NaiveProxy,
		Settings: `{"network":"tcp"}`,
		StreamSettings: `{"security":"tls","tlsSettings":{"serverName":"naive.example.com"}}`,
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	client := &model.ClientRecord{
		Email: "naive@example.com", SubID: subID, Enable: true,
		UUID: "11111111-2222-4333-8444-555555555555", Password: "",
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inbound.Id}).Error; err != nil {
		t.Fatalf("attach client: %v", err)
	}
	if err := db.Create(&xray.ClientTraffic{Email: client.Email, Up: 10, Down: 20, Enable: true}).Error; err != nil {
		t.Fatalf("seed traffic: %v", err)
	}

	links, _, _, traffic, err := NewSubService("").GetSubs(subID, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("links = %v, want no blank entry", links)
	}
	if traffic.Up != 10 || traffic.Down != 20 {
		t.Fatalf("traffic = up:%d down:%d, want up:10 down:20", traffic.Up, traffic.Down)
	}
}
func TestGetSubs_MixedNormalizedProtocols(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-mixed"
	db := database.GetDB()

	vless := &model.Inbound{
		UserId: 1, Tag: "multi-vless", Enable: true, Port: 42101, Protocol: model.VLESS,
		Settings: `{"clients":[{"id":"11111111-2222-4333-8444-555555555555","email":"vless@example.com","subId":"sub-mixed","enable":true}]}`,
		StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	trojan := &model.Inbound{
		UserId: 1, Tag: "multi-trojan", Enable: true, Port: 42102, Protocol: model.Trojan,
		Settings: `{"clients":[{"id":"66666666-7777-4888-8999-000000000000","email":"trojan@example.com","password":"secret","subId":"sub-mixed","enable":true}]}`,
		StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	if err := db.Create(vless).Error; err != nil {
		t.Fatalf("seed vless: %v", err)
	}
	if err := db.Create(trojan).Error; err != nil {
		t.Fatalf("seed trojan: %v", err)
	}

	vlessClient := &model.ClientRecord{Email: "vless@example.com", SubID: subID, UUID: "11111111-2222-4333-8444-555555555555", Enable: true}
	trojanClient := &model.ClientRecord{Email: "trojan@example.com", SubID: subID, UUID: "66666666-7777-4888-8999-000000000000", Password: "secret", Enable: true}
	if err := db.Create(vlessClient).Error; err != nil {
		t.Fatalf("seed vless client: %v", err)
	}
	if err := db.Create(trojanClient).Error; err != nil {
		t.Fatalf("seed trojan client: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: vlessClient.Id, InboundId: vless.Id}).Error; err != nil {
		t.Fatalf("attach vless: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: trojanClient.Id, InboundId: trojan.Id}).Error; err != nil {
		t.Fatalf("attach trojan: %v", err)
	}

	links, emails, _, _, err := NewSubService("").GetSubs(subID, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("links = %d, want 2: %v", len(links), links)
	}
	joined := strings.Join(links, "\n")
	if !strings.Contains(joined, "vless://") || !strings.Contains(joined, "trojan://") {
		t.Fatalf("subscription must contain VLESS and Trojan: %v", links)
	}
	if len(emails) != 2 || emails[0] != "vless@example.com" || emails[1] != "trojan@example.com" {
		t.Fatalf("emails = %v, want both normalized clients in inbound order", emails)
	}
}


func TestGetSubs_Hysteria2AndNaiveKeepSeparateConnections(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-hy-naive"
	const email = "shared@example.com"
	const uuid = "44444444-5555-4666-8777-888888888888"
	db := database.GetDB()

	hysteria := &model.Inbound{
		UserId: 1, Tag: "shared", Remark: "shared", Enable: true,
		Port: 42131, Listen: "hy.example.com", Protocol: model.Hysteria,
		Settings: fmt.Sprintf(`{"version":2,"clients":[{"email":%q,"subId":%q,"enable":true}]}`, email, subID),
		StreamSettings: `{"network":"hysteria","security":"tls","tlsSettings":{"serverName":"hy.example.com"}}`,
	}
	naive := &model.Inbound{
		UserId: 1, Tag: "shared-naive", Remark: "shared", Enable: true,
		Port: 42132, Listen: "naive.example.com", Protocol: model.NaiveProxy,
		Settings: fmt.Sprintf(`{"network":"tcp","tls":{"serverName":"naive.example.com"},"clients":[{"email":%q,"subId":%q,"enable":true}]}`, email, subID),
		StreamSettings: `{}`,
	}
	for _, inbound := range []*model.Inbound{hysteria, naive} {
		if err := db.Create(inbound).Error; err != nil {
			t.Fatalf("seed inbound %s: %v", inbound.Tag, err)
		}
	}

	client := &model.ClientRecord{
		Email: email, SubID: subID, UUID: uuid, Password: "naive-password",
		Auth: "hysteria-auth", Enable: true,
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	for _, inbound := range []*model.Inbound{hysteria, naive} {
		if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inbound.Id}).Error; err != nil {
			t.Fatalf("attach %s: %v", inbound.Protocol, err)
		}
	}

	links, _, _, _, err := NewSubService("").GetSubs(subID, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("links = %d, want 2: %v", len(links), links)
	}

	var hysteriaLink, naiveLink string
	for _, link := range links {
		switch {
		case strings.HasPrefix(link, "hysteria2://"):
			hysteriaLink = link
		case strings.HasPrefix(link, "naive+https://"), strings.HasPrefix(link, "naive://"):
			naiveLink = link
		}
	}
	if hysteriaLink == "" || naiveLink == "" {
		t.Fatalf("subscription must contain one Hysteria2 and one Naive link: %v", links)
	}
	if !strings.Contains(hysteriaLink, "#shared-hysteria2-") {
		t.Fatalf("Hysteria2 remark does not identify its protocol: %s", hysteriaLink)
	}
	if !strings.Contains(naiveLink, "naive.example.com:42132") || !strings.Contains(naiveLink, "shared%40example.com") {
		t.Fatalf("Naive link does not identify its connection: %s", naiveLink)
	}
	if hysteriaLink == naiveLink {
		t.Fatal("Hysteria2 and Naive links collapsed to the same connection")
	}
}

func TestGetSubs_VlessHysteria2AndNaiveStayIndependent(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-vless-hy-naive"
	const email = "shared-vh@example.com"
	const uuid = "11111111-2222-4333-8444-555555555555"
	db := database.GetDB()

	inbounds := []*model.Inbound{
		{
			UserId: 1, Tag: "vless", Remark: "shared", Enable: true,
			Port: 42130, Listen: "vless.example.com", Protocol: model.VLESS,
			Settings: fmt.Sprintf(`{"encryption":"none","clients":[{"id":%q,"email":%q,"subId":%q,"enable":true}]}`, uuid, email, subID),
			StreamSettings: `{"network":"tcp","security":"none"}`,
		},
		{
			UserId: 1, Tag: "hysteria", Remark: "shared", Enable: true,
			Port: 42131, Listen: "hy.example.com", Protocol: model.Hysteria,
			Settings: fmt.Sprintf(`{"version":2,"clients":[{"email":%q,"subId":%q,"enable":true}]}`, email, subID),
			StreamSettings: `{"network":"hysteria","security":"tls","tlsSettings":{"serverName":"hy.example.com"}}`,
		},
		{
			UserId: 1, Tag: "naive", Remark: "shared", Enable: true,
			Port: 42132, Listen: "naive.example.com", Protocol: model.NaiveProxy,
			Settings: fmt.Sprintf(`{"network":"tcp","tls":{"serverName":"naive.example.com"},"clients":[{"email":%q,"subId":%q,"enable":true}]}`, email, subID),
			StreamSettings: `{}`,
		},
	}
	for _, inbound := range inbounds {
		if err := db.Create(inbound).Error; err != nil {
			t.Fatalf("seed inbound %s: %v", inbound.Tag, err)
		}
	}

	client := &model.ClientRecord{
		Email: email, SubID: subID, UUID: uuid, Password: "naive-password",
		Auth: "hysteria-auth", Enable: true,
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	for _, inbound := range inbounds {
		if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inbound.Id}).Error; err != nil {
			t.Fatalf("attach %s: %v", inbound.Protocol, err)
		}
	}

	links, _, _, _, err := NewSubService("").GetSubs(subID, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}
	if len(links) != 3 {
		t.Fatalf("links = %d, want 3: %v", len(links), links)
	}

	var vlessLink, hysteriaLink, naiveLink string
	for _, link := range links {
		switch {
		case strings.HasPrefix(link, "vless://"):
			vlessLink = link
		case strings.HasPrefix(link, "hysteria2://"):
			hysteriaLink = link
		case strings.HasPrefix(link, "naive+https://"):
			naiveLink = link
		}
	}
	if vlessLink == "" || hysteriaLink == "" || naiveLink == "" {
		t.Fatalf("subscription must contain VLESS, Hysteria2 and Naive links: %v", links)
	}
	if !strings.Contains(vlessLink, uuid+"@vless.example.com:42130") {
		t.Fatalf("VLESS endpoint/UUID is wrong: %s", vlessLink)
	}
	if !strings.Contains(vlessLink, "encryption=none") {
		t.Fatalf("VLESS link must explicitly disable vlessenc when no encryption is configured: %s", vlessLink)
	}
	if !strings.Contains(hysteriaLink, "hysteria-auth@hy.example.com:42131") {
		t.Fatalf("Hysteria2 endpoint/auth is wrong: %s", hysteriaLink)
	}
	if !strings.Contains(naiveLink, "naive-password@naive.example.com:42132") {
		t.Fatalf("Naive endpoint/password is wrong: %s", naiveLink)
	}
	if vlessLink == hysteriaLink || vlessLink == naiveLink || hysteriaLink == naiveLink {
		t.Fatal("VLESS, Hysteria2 and Naive links must not collapse into one connection")
	}
}

func TestGetSubs_MultipleConnectionsSameProtocol(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-vless-multi"
	const uuid = "22222222-3333-4444-8555-666666666666"
	db := database.GetDB()
	client := &model.ClientRecord{Email: "multi-vless@example.com", SubID: subID, UUID: uuid, Enable: true}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}

	for _, seed := range []struct {
		tag  string
		port int
	}{
		{tag: "vless-one", port: 42111},
		{tag: "vless-two", port: 42112},
	} {
		ib := &model.Inbound{
			UserId: 1, Tag: seed.tag, Enable: true, Port: seed.port, Protocol: model.VLESS,
			Settings: fmt.Sprintf(`{"clients":[{"id":%q,"email":%q,"subId":%q,"enable":true}]}`, uuid, client.Email, subID),
			StreamSettings: `{"network":"tcp","security":"none"}`,
		}
		if err := db.Create(ib).Error; err != nil {
			t.Fatalf("seed inbound %s: %v", seed.tag, err)
		}
		if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: ib.Id}).Error; err != nil {
			t.Fatalf("attach %s: %v", seed.tag, err)
		}
	}

	links, emails, _, _, err := NewSubService("").GetSubs(subID, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("links = %d, want 2: %v", len(links), links)
	}
	for _, link := range links {
		if !strings.HasPrefix(link, "vless://") {
			t.Fatalf("unexpected subscription link: %s", link)
		}
	}
	if len(emails) != 2 || emails[0] != client.Email || emails[1] != client.Email {
		t.Fatalf("emails = %v, want the same client once per attached inbound", emails)
	}
	if links[0] == links[1] {
		t.Fatalf("two attached VLESS connections collapsed to the same link: %v", links)
	}
}

func TestGetSubs_DoesNotIncludeUnattachedSettingsOnlyInbound(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-no-legacy-leak"
	const uuid = "33333333-4444-4555-8666-777777777777"
	db := database.GetDB()

	working := &model.Inbound{
		UserId: 1, Tag: "working-vless", Enable: true, Port: 42121, Protocol: model.VLESS,
		Settings: fmt.Sprintf(`{"clients":[{"id":%q,"email":"user@example.com","subId":%q,"enable":true}]}`, uuid, subID),
		StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	stale := &model.Inbound{
		UserId: 1, Tag: "stale-trojan", Enable: true, Port: 42122, Protocol: model.Trojan,
		Settings: fmt.Sprintf(`{"clients":[{"id":%q,"email":"user@example.com","enable":true,"password":"stale-secret"}]}`, uuid),
		StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	if err := db.Create(working).Error; err != nil {
		t.Fatalf("seed working inbound: %v", err)
	}
	if err := db.Create(stale).Error; err != nil {
		t.Fatalf("seed stale inbound: %v", err)
	}

	client := &model.ClientRecord{Email: "user@example.com", SubID: subID, UUID: uuid, Enable: true}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: working.Id}).Error; err != nil {
		t.Fatalf("attach working inbound: %v", err)
	}

	links, _, _, _, err := NewSubService("").GetSubs(subID, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}
	if len(links) != 1 || !strings.HasPrefix(links[0], "vless://") {
		t.Fatalf("links = %v, want only the attached VLESS connection", links)
	}
	if strings.Contains(strings.Join(links, "\n"), "trojan://") {
		t.Fatal("subscription included an unattached legacy Trojan connection")
	}

	var attachments int64
	if err := db.Model(&model.ClientInbound{}).
		Where("client_id = ? AND inbound_id = ?", client.Id, stale.Id).
		Count(&attachments).Error; err != nil {
		t.Fatalf("count stale attachment: %v", err)
	}
	if attachments != 0 {
		t.Fatalf("subscription fetch unexpectedly created a client_inbound row: %d", attachments)
	}
}

func TestGetSubs_NaiveUsesHostEndpoints(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-naive-hosts"
	const email = "naive-host@example.com"
	db := database.GetDB()

	inbound := &model.Inbound{
		UserId: 1, Tag: "naive-hosts", Remark: "naive", Enable: true,
		Port: 443, Listen: "origin.example.com", Protocol: model.NaiveProxy,
		Settings: fmt.Sprintf(`{"network":"tcp","shareLinkFormat":"hiddify","tls":{"serverName":"origin.example.com"},"clients":[{"email":%q,"subId":%q,"enable":true}]}`, email, subID),
		StreamSettings: `{}`,
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	client := &model.ClientRecord{Email: email, SubID: subID, Password: "naive-secret", Enable: true}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inbound.Id}).Error; err != nil {
		t.Fatalf("attach client: %v", err)
	}
	hosts := []*model.Host{
		{InboundId: inbound.Id, Remark: "edge-a", Address: "edge-a.example.com", Port: 8443, Security: "tls", Sni: "origin.example.com", HostHeader: "origin.example.com"},
		{InboundId: inbound.Id, Remark: "edge-b", Address: "edge-b.example.com", Port: 9443, Security: "tls", Sni: "origin.example.com", HostHeader: "origin.example.com"},
	}
	for _, host := range hosts {
		if err := db.Create(host).Error; err != nil {
			t.Fatalf("seed host %s: %v", host.Remark, err)
		}
	}

	links, _, _, _, err := NewSubService("").GetSubs(subID, "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs: %v", err)
	}
	all := strings.Split(strings.Join(links, "\n"), "\n")
	got := make(map[string]bool)
	for _, link := range all {
		if strings.TrimSpace(link) == "" {
			continue
		}
		if !strings.HasPrefix(link, "naive+https://") && !strings.HasPrefix(link, "naive://") {
			t.Fatalf("unexpected Naive link: %s", link)
		}
		got[link] = true
	}
	if len(got) != 2 {
		t.Fatalf("got %d Naive links, want 2: %v", len(got), all)
	}
	joined := strings.Join(all, "\n")
	for _, want := range []string{
		"edge-a.example.com:8443",
		"edge-b.example.com:9443",
		"sni=origin.example.com",
		"host=origin.example.com",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("Naive host override missing %q in %v", want, all)
		}
	}
}


func TestGetSingBoxJsonResolvesTUICWildcardListen(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-tuic-wildcard"
	db := database.GetDB()
	inbound := &model.Inbound{
		UserId: 1, Tag: "tuic-wildcard", Enable: true, Listen: "0.0.0.0", Port: 443,
		Protocol: model.TUIC,
		Settings: `{"clients":[{"id":"11111111-2222-4333-8444-555555555555","email":"tuic-wildcard@example.com","password":"secret","enable":true}]}`,
		StreamSettings: `{"network":"tcp","security":"tls","tlsSettings":{"serverName":"tuic.example.com"}}`,
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	client := &model.ClientRecord{
		Email: "tuic-wildcard@example.com", SubID: subID,
		UUID: "11111111-2222-4333-8444-555555555555", Password: "secret", Enable: true,
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inbound.Id}).Error; err != nil {
		t.Fatalf("attach client: %v", err)
	}

	out, _, err := NewSubJsonService("", "", "", "", NewSubService("")).GetSingBoxJson(subID, "sub.example.com", false)
	if err != nil {
		t.Fatalf("GetSingBoxJson: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(out), &config); err != nil {
		t.Fatalf("unmarshal sing-box config: %v", err)
	}
	outbounds, _ := config["outbounds"].([]any)
	if len(outbounds) == 0 {
		t.Fatalf("sing-box config has no outbounds: %s", out)
	}
	proxy, _ := outbounds[0].(map[string]any)
	if got := proxy["server"]; got != "sub.example.com" {
		t.Fatalf("TUIC server = %v, want advertised request host instead of wildcard listen", got)
	}
}

func TestGetJsonFallsBackFromPartialExternalProxy(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	const subID = "sub-partial-external"
	db := database.GetDB()
	inbound := &model.Inbound{
		UserId: 1, Tag: "vless-partial", Enable: true, Listen: "0.0.0.0", Port: 443,
		Protocol: model.VLESS,
		Settings: `{"clients":[{"id":"22222222-3333-4444-8555-666666666666","email":"partial@example.com","subId":"sub-partial-external","enable":true}]}`,
		StreamSettings: `{"network":"tcp","security":"tls","tlsSettings":{"serverName":"partial.example.com"},"externalProxy":[{"remark":"fallback"}]}`,
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	client := &model.ClientRecord{
		Email: "partial@example.com", SubID: subID,
		UUID: "22222222-3333-4444-8555-666666666666", Enable: true,
	}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inbound.Id}).Error; err != nil {
		t.Fatalf("attach client: %v", err)
	}

	out, _, err := NewSubJsonService("", "", "", "", NewSubService("")).GetJson(subID, "sub.example.com", false)
	if err != nil {
		t.Fatalf("GetJson: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(out), &config); err != nil {
		t.Fatalf("unmarshal JSON subscription: %v", err)
	}
	outbounds, _ := config["outbounds"].([]any)
	if len(outbounds) == 0 {
		t.Fatalf("JSON subscription has no outbounds: %s", out)
	}
	proxy, _ := outbounds[0].(map[string]any)
	settings, _ := proxy["settings"].(map[string]any)
	if got := settings["address"]; got != "sub.example.com" {
		t.Fatalf("partial externalProxy address = %v, want advertised request host", got)
	}
	if got := settings["port"]; got != float64(443) {
		t.Fatalf("partial externalProxy port = %v, want 443", got)
	}
}


func TestGetSingBoxJsonRejectsMixedWireGuard(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	serverPriv, _, err := wgutil.GenerateWireguardKeypair()
	if err != nil { t.Fatalf("server keypair: %v", err) }
	clientPriv, _, err := wgutil.GenerateWireguardKeypair()
	if err != nil { t.Fatalf("client keypair: %v", err) }

	const subID = "sub-mixed-wg"
	db := database.GetDB()
	wgInbound := &model.Inbound{
		UserId: 1, Tag: "mixed-wg", Enable: true, Listen: "0.0.0.0", Port: 51820,
		Protocol: model.WireGuard, Settings: "{\"secretKey\":\"" + serverPriv + "\"}",
	}
	vlessInbound := &model.Inbound{
		UserId: 1, Tag: "mixed-vless", Enable: true, Listen: "0.0.0.0", Port: 443,
		Protocol: model.VLESS,
		Settings: "{\"clients\":[{\"id\":\"33333333-4444-4555-8666-777777777777\",\"email\":\"mixed@example.com\",\"subId\":\"" + subID + "\",\"enable\":true}]}",
		StreamSettings: "{\"network\":\"tcp\",\"security\":\"none\"}",
	}
	if err := db.Create(wgInbound).Error; err != nil { t.Fatalf("seed WireGuard inbound: %v", err) }
	if err := db.Create(vlessInbound).Error; err != nil { t.Fatalf("seed VLESS inbound: %v", err) }
	client := &model.ClientRecord{
		Email: "mixed@example.com", SubID: subID, UUID: "33333333-4444-4555-8666-777777777777",
		PrivateKey: clientPriv, AllowedIPs: "10.0.0.2/32", Enable: true,
	}
	if err := db.Create(client).Error; err != nil { t.Fatalf("seed client: %v", err) }
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: wgInbound.Id}).Error; err != nil { t.Fatalf("attach WireGuard: %v", err) }
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: vlessInbound.Id}).Error; err != nil { t.Fatalf("attach VLESS: %v", err) }

	_, _, err = NewSubJsonService("", "", "", "", NewSubService("")).GetSingBoxJson(subID, "sub.example.com", false)
	if !errors.Is(err, errSubscriptionFormatUnsupported) {
		t.Fatalf("GetSingBoxJson error = %v, want errSubscriptionFormatUnsupported", err)
	}
}
