package service

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestSingBoxInboundRequiresUsers(t *testing.T) {
	tests := []struct {
		name     string
		protocol model.Protocol
		want     bool
	}{
		{name: "vless", protocol: model.VLESS, want: true},
		{name: "vmess", protocol: model.VMESS, want: true},
		{name: "trojan", protocol: model.Trojan, want: true},
		{name: "naive", protocol: model.NaiveProxy, want: true},
		{name: "hysteria", protocol: model.Hysteria, want: true},
		{name: "shadowtls", protocol: model.ShadowTLS, want: true},
		{name: "anytls", protocol: model.AnyTLS, want: true},
		{name: "shadowsocks", protocol: model.Shadowsocks, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := singBoxInboundRequiresUsers(tt.protocol); got != tt.want {
				t.Fatalf("singBoxInboundRequiresUsers(%v) = %v, want %v", tt.protocol, got, tt.want)
			}
		})
	}
}

func TestSingBoxExcludesLocalSidecarInbounds(t *testing.T) {
	for _, protocol := range []model.Protocol{
		model.MTProto, model.AmneziaWG, model.TUIC, model.Mieru,
		model.Pingtunnel, model.TrustTunnel, model.Sudoku, model.VKTurnProxy,
	} {
		if !isLocalSidecarInbound(protocol) {
			t.Errorf("%s must be handled by its local sidecar", protocol)
		}
	}
	for _, protocol := range []model.Protocol{
		model.VLESS, model.VMESS, model.Trojan, model.Shadowsocks,
		model.AnyTLS, model.ShadowTLS, model.HTTP, model.Mixed,
	} {
		if isLocalSidecarInbound(protocol) {
			t.Errorf("%s must remain available to sing-box", protocol)
		}
	}
}
