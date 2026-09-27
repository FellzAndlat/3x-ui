package mieru

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestInstanceFromInbound(t *testing.T) {
	inbound := &model.Inbound{
		Id:       42,
		Tag:      "mieru-test",
		Port:     8443,
		Listen:   "0.0.0.0",
		Protocol: model.Mieru,
		Settings: `{"protocols":["TCP","UDP"],"additionalPorts":[9443],"mtu":1400,"loggingLevel":"DEBUG","userHintIsMandatory":true,"clients":[{"email":"alice","password":"secret","enable":true},{"email":"disabled","password":"x","enable":false}]}`,
	}
	inst, ok := InstanceFromInbound(inbound)
	if !ok {
		t.Fatal("InstanceFromInbound returned false")
	}
	if len(inst.PortBindings) != 4 {
		t.Fatalf("port bindings = %d, want 4", len(inst.PortBindings))
	}
	if len(inst.Users) != 1 || inst.Users[0].Name != "alice" {
		t.Fatalf("users = %#v, want alice only", inst.Users)
	}
	if inst.MTU != 1400 || inst.LoggingLevel != "DEBUG" || !inst.UserHintIsMandatory {
		t.Fatalf("unexpected settings: %#v", inst)
	}
}

func TestInstanceFromInboundRejectsWithoutUsers(t *testing.T) {
	inbound := &model.Inbound{
		Port:     443,
		Protocol: model.Mieru,
		Settings: `{"protocols":["TCP"],"clients":[]}`,
	}
	if _, ok := InstanceFromInbound(inbound); ok {
		t.Fatal("expected inbound without enabled users to be rejected")
	}
}

func TestValidatePortBindings(t *testing.T) {
	for _, tc := range []struct {
		name, settings string
		port           int
		valid          bool
	}{
		{"valid primary", `{}`, 2200, true},
		{"privileged primary", `{}`, 443, false},
		{"valid explicit", `{"tcpPorts":["2100-2102"],"udpPorts":["2200"]}`, 443, true},
		{"invalid explicit", `{"tcpPorts":["443","2100"]}`, 2200, false},
		{"empty explicit with invalid primary", `{"tcpPorts":[]}`, 443, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ib := &model.Inbound{Protocol: model.Mieru, Port: tc.port, Settings: tc.settings}
			if err := ValidatePortBindings(ib); (err == nil) != tc.valid {
				t.Fatalf("ValidatePortBindings error = %v, want valid %v", err, tc.valid)
			}
		})
	}
}

func TestRenderConfigUsesServerSettingsOnly(t *testing.T) {
	inst := Instance{
		PortBindings: []PortBinding{{Port: 8443, Protocol: "TCP"}},
		Users:        []User{{Name: "alice", Password: "secret"}},
		MTU:          1400,
		LoggingLevel: "INFO",
	}
	cfg := renderConfig(inst)
	if _, ok := cfg["multiplexing"]; ok {
		t.Fatal("server config must not contain client-only multiplexing")
	}
	if _, ok := cfg["handshakeMode"]; ok {
		t.Fatal("server config must not contain client-only handshakeMode")
	}
}
