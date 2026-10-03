package service

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

// Password-based inbounds can attach an existing identity without a UUID.
// The password, not client.ID, is the credential validated by these protocols.
func TestCreatePasswordProtocolClientsWithoutUUID(t *testing.T) {
	for _, protocol := range []model.Protocol{
		model.NaiveProxy, model.AnyTLS, model.ShadowTLS, model.Mieru,
	} {
		t.Run(string(protocol), func(t *testing.T) {
			setupBulkDB(t)
			inbound := mkInbound(t, 34001, protocol, `{"clients":[]}`)
			client := model.Client{Email: "password@x", SubID: "password-sub", Password: "secret", Enable: true}
			if _, err := (&ClientService{}).Create(&InboundService{}, &ClientCreatePayload{
				Client: client, InboundIds: []int{inbound.Id},
			}); err != nil {
				t.Fatalf("Create on %s without UUID: %v", protocol, err)
			}
			if emails := settingsClientEmails(t, inbound.Id); len(emails) != 1 || emails[0] != client.Email {
				t.Fatalf("attached clients = %v", emails)
			}
		})
	}
}

func TestAttachPasswordProtocolWithoutUUID(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}
	source := mkInbound(t, 34002, model.NaiveProxy, `{"clients":[]}`)
	target := mkInbound(t, 34003, model.ShadowTLS, `{"clients":[]}`)
	if _, err := svc.Create(inboundSvc, &ClientCreatePayload{
		Client:     model.Client{Email: "attached@x", SubID: "attached-sub", Password: "secret", Enable: true},
		InboundIds: []int{source.Id},
	}); err != nil {
		t.Fatalf("seed client: %v", err)
	}
	var rec model.ClientRecord
	if err := database.GetDB().Where("email = ?", "attached@x").First(&rec).Error; err != nil {
		t.Fatal(err)
	}
	if rec.UUID != "" {
		t.Fatalf("password-only client unexpectedly gained UUID %q", rec.UUID)
	}
	if _, err := svc.Attach(inboundSvc, rec.Id, []int{target.Id}); err != nil {
		t.Fatalf("attach ShadowTLS without UUID: %v", err)
	}
	if emails := settingsClientEmails(t, target.Id); len(emails) != 1 || emails[0] != rec.Email {
		t.Fatalf("ShadowTLS clients = %v", emails)
	}
}
