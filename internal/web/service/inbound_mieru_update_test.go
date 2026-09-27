package service

import (
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestUpdateLocalMieruSettingsDoesNotDereferenceNodeID(t *testing.T) {
	setupConflictDB(t)
	seedInboundConflict(t, "mieru-settings", "0.0.0.0", 32000, model.Mieru,
		`{"network":"tcp"}`, `{"protocols":["TCP"],"clients":[]}`)

	var existing model.Inbound
	if err := database.GetDB().Where("tag = ?", "mieru-settings").First(&existing).Error; err != nil {
		t.Fatal(err)
	}
	update := existing
	update.Settings = `{"protocols":["TCP","UDP"],"clients":[]}`
	if _, _, err := (&InboundService{}).UpdateInbound(&update); err != nil {
		t.Fatalf("save local Mieru settings: %v", err)
	}

	var saved model.Inbound
	if err := database.GetDB().First(&saved, existing.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.NodeID != nil || !strings.Contains(saved.Settings, `"UDP"`) {
		t.Fatalf("settings or local node assignment changed: %+v", saved)
	}
}
