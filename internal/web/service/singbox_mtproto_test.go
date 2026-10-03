package service

import (
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"testing"
)

func TestSingBoxMtprotoEgress(t *testing.T) {
	for _, endpoint := range []bool{false, true} {
		cfg := &singbox.Config{Route: map[string]any{"rules": []map[string]any{{"action": "reject"}}}}
		targets := []map[string]any{{"tag": "egress"}}
		if endpoint {
			cfg.Endpoints = targets
		} else {
			cfg.Outbounds = targets
		}
		ib := &model.Inbound{Enable: true, Protocol: model.MTProto, Tag: "telegram", Settings: `{"routeThroughXray":true,"routeXrayPort":32123,"outboundTag":"egress"}`}
		if err := injectSingBoxMtprotoEgress(cfg, ib); err != nil {
			t.Fatal(err)
		}
		if len(cfg.Inbounds) != 1 || cfg.Inbounds[0]["listen"] != "127.0.0.1" || cfg.Inbounds[0]["type"] != "socks" {
			t.Fatalf("bad bridge: %#v", cfg.Inbounds)
		}
		rules := cfg.Route["rules"].([]map[string]any)
		if len(rules) != 2 || rules[0]["outbound"] != "egress" {
			t.Fatalf("selected egress must precede template rules: %#v", rules)
		}
	}
	ib := &model.Inbound{Enable: true, Protocol: model.MTProto, Tag: "telegram", Settings: `{"routeThroughXray":true,"routeXrayPort":32123,"outboundTag":"missing"}`}
	if err := injectSingBoxMtprotoEgress(&singbox.Config{}, ib); err == nil {
		t.Fatal("missing target accepted")
	}
	ib.Settings = `{"routeThroughXray":true,"routeXrayPort":32123}`
	cfg := &singbox.Config{}
	if err := injectSingBoxMtprotoEgress(cfg, ib); err != nil {
		t.Fatal(err)
	}
	if cfg.Route != nil || len(cfg.Inbounds) != 1 {
		t.Fatal("unselected egress must use normal routing")
	}
	ib.Enable = false
	cfg = &singbox.Config{}
	if err := injectSingBoxMtprotoEgress(cfg, ib); err != nil || len(cfg.Inbounds) != 0 {
		t.Fatal("disabled inbound installed a bridge")
	}
}
