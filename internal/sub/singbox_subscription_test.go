package sub

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestBuildSeparatedSingBoxSubscription_MultipleProxies(t *testing.T) {
	proxies := []map[string]any{
		{"type": "vless", "tag": "vless-user", "server": "vless.example.com", "server_port": 443},
		{"type": "trojan", "tag": "trojan-user", "server": "trojan.example.com", "server_port": 8443},
	}

	got, err := buildSeparatedSingBoxSubscription(nil, proxies, false)
	if err != nil {
		t.Fatalf("buildSeparatedSingBoxSubscription: %v", err)
	}

	var docs []map[string]any
	if err := json.Unmarshal([]byte(got), &docs); err != nil {
		t.Fatalf("subscription is not a JSON array: %v\n%s", err, got)
	}
	if len(docs) != 2 {
		t.Fatalf("profile count = %d, want 2: %s", len(docs), got)
	}

	wantTypes := []string{"vless", "trojan"}
	for i, doc := range docs {
		outs, ok := doc["outbounds"].([]any)
		if !ok {
			t.Fatalf("profile %d outbounds = %#v, want array", i, doc["outbounds"])
		}
		if len(outs) != 3 {
			t.Fatalf("profile %d outbound count = %d, want 3", i, len(outs))
		}
		first, ok := outs[0].(map[string]any)
		if !ok {
			t.Fatalf("profile %d first outbound = %#v", i, outs[0])
		}
		if first["type"] != wantTypes[i] {
			t.Fatalf("profile %d type = %v, want %s", i, first["type"], wantTypes[i])
		}
		if _, ok := first["server"]; !ok {
			t.Fatalf("profile %d proxy missing server: %#v", i, first)
		}
		for _, raw := range outs[1:] {
			ob, _ := raw.(map[string]any)
			if ob["type"] == "selector" || ob["type"] == "urltest" {
				t.Fatalf("profile %d unexpectedly contains merged selector/urltest outbound: %#v", i, ob)
			}
		}
	}
}

func TestBuildSeparatedSingBoxSubscription_AlwaysReturnArrayForSingleProxy(t *testing.T) {
	got, err := buildSeparatedSingBoxSubscription(nil, []map[string]any{
		{"type": "vless", "tag": "vless-user", "server": "example.com", "server_port": 443},
	}, true)
	if err != nil {
		t.Fatalf("buildSeparatedSingBoxSubscription: %v", err)
	}

	var docs []map[string]any
	if err := json.Unmarshal([]byte(got), &docs); err != nil {
		t.Fatalf("subscription is not a JSON array: %v\n%s", err, got)
	}
	if len(docs) != 1 {
		t.Fatalf("profile count = %d, want 1", len(docs))
	}
}

func TestBuildShadowTLSSubscriptionsKeepTransportAndVisibleSOCKS(t *testing.T) {
	makeProxy := func(tag string) map[string]any {
		return map[string]any{
			"type": "socks", "tag": tag, "username": tag, "password": "secret",
			"server": "127.0.0.1", "server_port": 1, "detour": "§hide§ shadowtls-transport",
			"_panel_shadowtls_transport": map[string]any{
				"type": "shadowtls", "tag": "§hide§ shadowtls-transport",
				"server": "edge.example.com", "server_port": 443, "version": 3, "password": "secret",
			},
		}
	}
	separated, err := buildSeparatedSingBoxSubscription(nil, []map[string]any{makeProxy("alice")}, false)
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	if err := json.Unmarshal([]byte(separated), &profile); err != nil {
		t.Fatal(err)
	}
	outs := profile["outbounds"].([]any)
	if len(outs) != 4 || outs[0].(map[string]any)["type"] != "socks" || outs[1].(map[string]any)["type"] != "shadowtls" {
		t.Fatalf("invalid standalone ShadowTLS profile: %s", separated)
	}
	if _, leaked := outs[0].(map[string]any)["_panel_shadowtls_transport"]; leaked {
		t.Fatalf("internal metadata leaked into profile: %s", separated)
	}

	hiddify, err := buildHiddifySingBoxSubscription([]map[string]any{makeProxy("alice"), makeProxy("bob")})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(hiddify), &profile); err != nil {
		t.Fatalf("Hiddify importer requires a single object: %v: %s", err, hiddify)
	}
	outs = profile["outbounds"].([]any)
	if len(outs) != 4 {
		t.Fatalf("Hiddify outbound count: %s", hiddify)
	}
	for i := 0; i < 4; i += 2 {
		transport := outs[i].(map[string]any)
		proxy := outs[i+1].(map[string]any)
		if transport["type"] != "shadowtls" || proxy["type"] != "socks" || proxy["detour"] != transport["tag"] {
			t.Fatalf("broken Hiddify transport binding: %s", hiddify)
		}
	}
	if outs[0].(map[string]any)["tag"] == outs[2].(map[string]any)["tag"] {
		t.Fatalf("ShadowTLS transport tags are not unique: %s", hiddify)
	}
}


func TestBuildSeparatedSingBoxSubscriptionFailsClosedOnRoutingTranslation(t *testing.T) {
	template := map[string]any{
		"routing": map[string]any{
			"rules": []any{
				map[string]any{
					"network":     "icmp",
					"outboundTag": "direct",
				},
			},
		},
	}
	_, err := buildSeparatedSingBoxSubscription(template, []map[string]any{
		{"type": "vless", "tag": "vless-user", "server": "example.com", "server_port": 443},
	}, false)
	if !errors.Is(err, errSubscriptionFormatUnsupported) {
		t.Fatalf("error = %v, want errSubscriptionFormatUnsupported", err)
	}
}


func TestGenNativeNaivePreservesNativeType(t *testing.T) {
	service := &SubJsonService{}
	inbound := &model.Inbound{
		Protocol: model.NaiveProxy,
		Listen:   "naive.example.com",
		Port:     443,
		Settings: `{"network":"tcp","tls":{"serverName":"naive.example.com","certificatePath":"/cert.pem","keyPath":"/key.pem"},"clients":[{"email":"user","password":"secret"}]}`,
		StreamSettings: `{"security":"tls"}`,
	}
	subReq := &SubService{}
	raw := service.genNativeNaive(subReq, inbound, model.Client{Email: "user", Password: "secret"}, nil)
	if raw == nil {
		t.Fatal("genNativeNaive returned nil")
	}
	got := raw
	if got["type"] != "naive" {
		t.Fatalf("type = %v, want naive; config=%#v", got["type"], got)
	}
	if port, ok := got["server_port"].(int); !ok || port != 443 {
		t.Fatalf("server_port = %v, want int(443)", got["server_port"])
	}
	if got["username"] != "user" || got["password"] != "secret" {
		t.Fatalf("credentials = (%v, %v), want user/secret", got["username"], got["password"])
	}
	tls, ok := got["tls"].(map[string]any)
	if !ok || tls["enabled"] != true {
		t.Fatalf("tls = %#v, want enabled=true", got["tls"])
	}
}

func TestGenNativeTUICPreservesClientSettings(t *testing.T) {
	svc := &SubJsonService{}
	inbound := &model.Inbound{
		Protocol: model.TUIC,
		Listen: "tuic.example.com",
		Port: 443,
		Settings: `{"certificate":"/cert.pem","private_key":"/key.pem","congestion_control":"bbr","udp_relay_mode":"quic","zero_rtt_handshake":true,"clients":[{"uuid":"11111111-2222-3333-4444-555555555555","password":"secret","email":"user","enable":true}]}`,
		StreamSettings: `{"security":"tls","tlsSettings":{"serverName":"tuic.example.com","alpn":["h3"]}}`,
	}
	raw := svc.genNativeTUIC(inbound, unmarshalStreamSettings(inbound.StreamSettings), model.Client{ID:"11111111-2222-3333-4444-555555555555",Password:"secret",Email:"user"})
	if raw == nil {
		t.Fatal("genNativeTUIC returned nil")
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal native TUIC: %v", err)
	}
	for key, want := range map[string]any{
		"congestion_control": "bbr",
		"udp_relay_mode":     "quic",
		"zero_rtt_handshake":  true,
	} {
		if got[key] != want {
			t.Fatalf("%s = %v, want %v; config=%#v", key, got[key], want, got)
		}
	}
}


func TestGenNativeAnyTLSUsesClientPassword(t *testing.T) {
	svc := &SubJsonService{}
	inbound := &model.Inbound{
		Protocol: model.AnyTLS,
		Listen:   "anytls.example.com",
		Port:     443,
		Settings: `{"tls":{"serverName":"anytls.example.com"},"clients":[{"email":"user","password":"secret"}]}`,
	}
	subReq := &SubService{}
	got := svc.genNativeTLSLike(subReq, inbound, model.Client{Email: "user", Password: "secret"})
	if got == nil {
		t.Fatal("genNativeTLSLike returned nil")
	}
	if got["type"] != "anytls" || got["server"] != "anytls.example.com" ||
		got["server_port"] != 443 || got["password"] != "secret" {
		t.Fatalf("unexpected AnyTLS outbound: %#v", got)
	}
	tls, ok := got["tls"].(map[string]any)
	if !ok || tls["server_name"] != "anytls.example.com" {
		t.Fatalf("unexpected AnyTLS TLS: %#v", got["tls"])
	}
}

func TestGenNativeShadowTLSUsesHandshakeServer(t *testing.T) {
	svc := &SubJsonService{}
	inbound := &model.Inbound{
		Protocol: model.ShadowTLS,
		Listen:   "shadowtls.example.com",
		Port:     443,
		Settings: `{"version":3,"handshake":{"server":"cloudflare.com","serverPort":443},"clients":[{"email":"user","password":"secret"}]}`,
	}
	subReq := &SubService{}
	got := svc.genNativeTLSLike(subReq, inbound, model.Client{Email: "user", Password: "secret"})
	if got == nil {
		t.Fatal("genNativeTLSLike returned nil")
	}
	if got["type"] != "socks" || got["username"] != "user" || got["password"] != "secret" {
		t.Fatalf("unexpected inner SOCKS outbound: %#v", got)
	}
	transport, ok := got["_panel_shadowtls_transport"].(map[string]any)
	if !ok || transport["type"] != "shadowtls" || transport["server"] != "shadowtls.example.com" ||
		transport["server_port"] != 443 || transport["version"] != 3 || transport["password"] != "secret" {
		t.Fatalf("unexpected ShadowTLS transport: %#v", transport)
	}
	tls, ok := transport["tls"].(map[string]any)
	if !ok || tls["server_name"] != "cloudflare.com" {
		t.Fatalf("unexpected ShadowTLS TLS: %#v", got["tls"])
	}
}

func TestGenNativeShadowTLSExternalEndpoint(t *testing.T) {
	svc := &SubJsonService{}
	inbound := &model.Inbound{Protocol: model.ShadowTLS, Listen: "origin.example.com", Port: 443,
		Settings: `{"version":3,"handshake":{"server":"cloudflare.com"}}`}
	subReq := &SubService{}
	endpoint := ShareEndpoint{Address: "edge.example.com", Port: 8443, ep: map[string]any{
		"sni": "front.example.com", "allowInsecure": true, "alpn": []any{"h2"},
	}}
	got := svc.genNativeTLSLikeEndpoint(subReq, inbound, model.Client{Email: "user", Password: "secret"}, endpoint)
	if got == nil {
		t.Fatal("genNativeTLSLikeEndpoint returned nil")
	}
	transport, _ := got["_panel_shadowtls_transport"].(map[string]any)
	if transport["server"] != "edge.example.com" || transport["server_port"] != 8443 {
		t.Fatalf("unexpected ShadowTLS endpoint: %#v", got)
	}
	tls := transport["tls"].(map[string]any)
	if tls["server_name"] != "front.example.com" {
		t.Fatalf("ShadowTLS endpoint SNI = %#v", tls)
	}
	if tls["insecure"] != true || !reflect.DeepEqual(tls["alpn"], []any{"h2"}) {
		t.Fatalf("ShadowTLS endpoint TLS options = %#v", tls)
	}
}

func TestGenNativeNaiveUsesAdvertisedDomainAsTLSName(t *testing.T) {
	svc := &SubJsonService{}
	inbound := &model.Inbound{Protocol: model.NaiveProxy, Listen: "naive.example.com", Port: 443,
		Settings: `{"network":"tcp","tls":{}}`}
	got := svc.genNativeNaive(&SubService{}, inbound, model.Client{Email: "user", Password: "secret"}, nil)
	if got == nil {
		t.Fatal("genNativeNaive returned nil")
	}
	tls := got["tls"].(map[string]any)
	if tls["server_name"] != "naive.example.com" {
		t.Fatalf("Naive TLS server_name = %v", tls["server_name"])
	}
}

func TestShadowTLSLinkSkipsPlaintextEndpoint(t *testing.T) {
	inbound := &model.Inbound{Protocol: model.ShadowTLS, Listen: "shadow.example.com", Port: 443,
		Settings: `{"handshake":{"server":"cloudflare.com"},"clients":[{"email":"user","password":"secret"}]}`,
		StreamSettings: `{"externalProxy":[{"dest":"plain.example.com","port":80,"forceTls":"none"}]}`}
	if link := (&SubService{}).genShadowTlsLink(inbound, "user"); link != "" {
		t.Fatalf("ShadowTLS must not advertise a plaintext endpoint: %q", link)
	}
}
