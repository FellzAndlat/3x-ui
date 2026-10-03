package sub

import (
	"net/url"
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestGenWireguardLinkPreservesAllAllowedIPs(t *testing.T) {
	inbound := &model.Inbound{
		Id:       1,
		Protocol: model.WireGuard,
		Port:     51820,
		Listen:   "0.0.0.0",
		Settings: `{"secretKey":""}`,
	}
	client := model.Client{
		Email:      "wg@example.com",
		PrivateKey: "client-private-key",
		AllowedIPs: []string{"10.0.0.2/32", "fd00::2/128"},
	}

	s := &SubService{
		address:             "vpn.example.com",
		clientsByInbound:    map[int]map[string]model.Client{1: {client.Email: client}},
		fullyPrimedInbounds: map[int]bool{1: true},
		settingsByInbound:   map[int]map[string]any{},
	}

	link := s.genWireguardLink(inbound, client.Email)
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := parsed.Query().Get("address"); got != strings.Join(client.AllowedIPs, ",") {
		t.Fatalf("address = %q, want %q", got, strings.Join(client.AllowedIPs, ","))
	}
}

func TestGenWireguardLinkFormatsIPv6Endpoint(t *testing.T) {
	inbound := &model.Inbound{
		Id:       2,
		Protocol: model.WireGuard,
		Port:     51820,
		Listen:   "::",
		Settings: `{"secretKey":""}`,
	}
	client := model.Client{
		Email:      "wg6@example.com",
		PrivateKey: "client-private-key",
		AllowedIPs: []string{"fd00::2/128"},
	}

	s := &SubService{
		address:             "2001:db8::10",
		clientsByInbound:    map[int]map[string]model.Client{2: {client.Email: client}},
		fullyPrimedInbounds: map[int]bool{2: true},
		settingsByInbound:   map[int]map[string]any{},
	}

	link := s.genWireguardLink(inbound, client.Email)
	if !strings.Contains(link, "@[2001:db8::10]:51820") {
		t.Fatalf("link = %q, missing bracketed IPv6 endpoint", link)
	}
}

func TestGenWireguardLinkExternalProxyFanOut(t *testing.T) {
	inbound := &model.Inbound{
		Protocol:       model.WireGuard,
		Port:           51820,
		Listen:         "0.0.0.0",
		Settings:       `{"secretKey":""}`,
		StreamSettings: `{"externalProxy":[{"dest":"edge1.example.com","port":443,"remark":"E1"},{"dest":"","remark":"E2"}]}`,
	}
	client := model.Client{Email: "wg@example.com", PrivateKey: "client-private-key", AllowedIPs: []string{"10.0.0.2/32"}}
	s := &SubService{address: "vpn.example.com", clientsByInbound: map[int]map[string]model.Client{0: {client.Email: client}}, fullyPrimedInbounds: map[int]bool{0: true}, settingsByInbound: map[int]map[string]any{}}
	links := s.genWireguardLink(inbound, client.Email)
	parts := strings.Split(strings.TrimSpace(links), "\n")
	if len(parts) != 2 {
		t.Fatalf("links = %d, want 2: %q", len(parts), links)
	}
	if !strings.Contains(parts[0], "@edge1.example.com:443") {
		t.Fatalf("first endpoint mismatch: %s", parts[0])
	}
	if !strings.Contains(parts[1], "@vpn.example.com:51820") {
		t.Fatalf("partial endpoint must fall back to inbound endpoint: %s", parts[1])
	}
}

func TestGenWireguardLinkJSONIPv6Endpoint(t *testing.T) {
	inbound := &model.Inbound{
		Protocol: model.WireGuard,
		Listen:   "::",
		Port:     51820,
		Settings: `{"secretKey":""}`,
	}
	client := model.Client{
		PrivateKey: "client-private-key",
		AllowedIPs: []string{"fd00::2/128"},
		Email:      "wg6@example.com",
	}
	svc := NewSubJsonService("", "", "", "", nil)
	body := svc.genWireguard(inbound, client)
	if body == nil {
		t.Fatal("genWireguard returned nil")
	}
	if !strings.Contains(string(body), "\"endpoint\": \"[2001:db8::") && !strings.Contains(string(body), "\"endpoint\": \"[::]") {
		// The helper uses the inbound's resolved address; with no service address
		// it falls back to the bind address "::" and must still bracket it.
		if !strings.Contains(string(body), "\"endpoint\": \"[::]:51820\"") {
			t.Fatalf("WireGuard JSON IPv6 endpoint is not bracketed: %s", body)
		}
	}
}
