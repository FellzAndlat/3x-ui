package service

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func TestAuditGroupMovePreservesHistoryAndCountsNewUsage(t *testing.T) {
	initTrafficTestDB(t)
	svc := &ClientService{}
	seedGroupedClient(t, "old", "source", 100, 0)
	seedGroupedClient(t, "new", "source", 25, 0)
	if err := svc.ResetGroupTraffic("source"); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(&xray.ClientTraffic{}).Where("email = ?", "new").Update("up", 35).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddToGroup([]string{"old"}, "destination"); err != nil {
		t.Fatal(err)
	}
	if g := groupByName(t, svc, "source"); g.Up != 10 {
		t.Fatalf("source history = %d, want 10", g.Up)
	}
	if g := groupByName(t, svc, "destination"); g.Up != 0 {
		t.Fatalf("destination inherited old usage: %+v", g)
	}
	if err := database.GetDB().Model(&xray.ClientTraffic{}).Where("email = ?", "old").Update("up", 110).Error; err != nil {
		t.Fatal(err)
	}
	if g := groupByName(t, svc, "destination"); g.Up != 10 {
		t.Fatalf("new destination usage = %d, want 10", g.Up)
	}
}

func TestAuditGroupRenameRejectsDerivedDestination(t *testing.T) {
	initTrafficTestDB(t)
	svc := &ClientService{}
	seedGroupedClient(t, "a", "old", 100, 0)
	seedGroupedClient(t, "b", "existing", 25, 0)
	if err := svc.ResetGroupTraffic("old"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenameGroup("old", "existing"); err == nil {
		t.Fatal("rename merged two logical groups")
	}
	if err := svc.CreateGroup("existing"); err == nil {
		t.Fatal("created an already derived group")
	}
	if g := groupByName(t, svc, "existing"); g.Up != 25 || g.ClientCount != 1 {
		t.Fatalf("destination changed: %+v", g)
	}
	if _, err := svc.RenameGroup("old", "renamed"); err != nil {
		t.Fatal(err)
	}
	if g := groupByName(t, svc, "renamed"); g.Up != 0 || g.ClientCount != 1 {
		t.Fatalf("rename lost reset baseline: %+v", g)
	}
}

func TestAuditGroupMutationRollsBackMalformedProjection(t *testing.T) {
	for _, operation := range []string{"rename", "delete", "move"} {
		t.Run(operation, func(t *testing.T) {
			initTrafficTestDB(t)
			db := database.GetDB()
			svc := &ClientService{}
			seedGroupedClient(t, "a", "original", 100, 0)
			if err := svc.ResetGroupTraffic("original"); err != nil {
				t.Fatal(err)
			}
			var rec model.ClientRecord
			if err := db.Where("email = ?", "a").First(&rec).Error; err != nil {
				t.Fatal(err)
			}
			ib := &model.Inbound{UserId: 1, Port: 45001, Tag: "broken", Protocol: model.VLESS, Settings: "{"}
			if err := db.Create(ib).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.ClientInbound{ClientId: rec.Id, InboundId: ib.Id}).Error; err != nil {
				t.Fatal(err)
			}
			var err error
			switch operation {
			case "rename":
				_, err = svc.RenameGroup("original", "changed")
			case "delete":
				_, err = svc.DeleteGroup("original")
			case "move":
				_, err = svc.AddToGroup([]string{"a"}, "changed")
			}
			if err == nil {
				t.Fatal("malformed projection was silently accepted")
			}
			if g := groupByName(t, svc, "original"); g.Up != 0 || g.ClientCount != 1 {
				t.Fatalf("partial group mutation: %+v", g)
			}
			var count int64
			if err := db.Model(&model.ClientGroup{}).Where("name = ?", "changed").Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("failed mutation left destination metadata")
			}
		})
	}
}
