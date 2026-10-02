package service

import (
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/mtproto"
	"github.com/SawaMEN/3x-ui/v3/internal/web/runtime"
)

func TestClientCrudMtprotoAppliesImmediately(t *testing.T) {
	setupConflictDB(t)
	pidFile := installFakeMtg(t)
	runtime.SetManager(runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }}))
	t.Cleanup(func() { runtime.SetManager(nil) })

	inboundSvc := &InboundService{}
	clientSvc := &ClientService{}

	created, _, err := inboundSvc.AddInbound(&model.Inbound{
		Enable:   true,
		Listen:   "",
		Port:     46201,
		Protocol: model.MTProto,
		Settings: `{"clients":[{"email":"first","secret":"` + mtprotoTestSecretA + `","enable":true}]}`,
	})
	if err != nil {
		t.Fatalf("AddInbound: %v", err)
	}
	t.Cleanup(func() { mtproto.GetManager().Remove(created.Id) })
	waitForSpawns(t, pidFile, 1)

	t.Run("add client rewrites the served config", func(t *testing.T) {
		payload := &model.Inbound{
			Id:       created.Id,
			Settings: `{"clients":[{"email":"second","secret":"` + mtprotoTestSecretB + `","enable":true}]}`,
		}
		needRestart, err := clientSvc.AddInboundClient(inboundSvc, payload)
		if err != nil {
			t.Fatalf("AddInboundClient: %v", err)
		}
		if needRestart {
			t.Fatal("adding an mtproto client must not request an xray restart")
		}
		cfg := readTelemtConfig(t, created.Id)
		if !strings.Contains(cfg, `"second" = "101112131415161718191a1b1c1d1e1f"`) {
			t.Fatalf("new client must be in the Telemt config with its normalized base secret:\n%s", cfg)
		}
		if !strings.Contains(cfg, `"first" = "00112233445566778899aabbccddeeff"`) {
			t.Fatalf("existing client must remain served by Telemt:\n%s", cfg)
		}
	})

	t.Run("delete client drops it from the served config", func(t *testing.T) {
		if _, err := clientSvc.DelInboundClientByEmail(inboundSvc, created.Id, "second", false, true); err != nil {
			t.Fatalf("DelInboundClientByEmail: %v", err)
		}
		cfg := readTelemtConfig(t, created.Id)
		if strings.Contains(cfg, "101112131415161718191a1b1c1d1e1f") {
			t.Fatalf("deleted client must leave the Telemt config:\n%s", cfg)
		}
		if !strings.Contains(cfg, "00112233445566778899aabbccddeeff") {
			t.Fatalf("surviving client must stay served by Telemt:\n%s", cfg)
		}
	})
}
