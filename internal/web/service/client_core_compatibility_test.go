package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func TestSyncClientCoreCompatibilitySwitchesAutomaticClientState(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()
	makeInbound := func(tag string, protocol model.Protocol, port int, email string) *model.Inbound {
		payload, _ := json.Marshal(map[string][]model.Client{
			"clients": {{Email: email, Enable: true}},
		})
		ib := &model.Inbound{Tag: tag, Protocol: protocol, Port: port, Enable: true, Settings: string(payload)}
		if err := db.Create(ib).Error; err != nil {
			t.Fatal(err)
		}
		return ib
	}

	wg := makeInbound("wg-1", model.WireGuard, 51820, "wg@example")
	vless := makeInbound("vless-1", model.VLESS, 443, "vless@example")
	naive := makeInbound("naive-1", model.NaiveProxy, 8443, "naive@example")

	records := []model.ClientRecord{
		{Email: "wg@example", Enable: true},
		{Email: "vless@example", Enable: true},
		{Email: "naive@example", Enable: true},
	}
	for i := range records {
		if err := db.Create(&records[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, link := range []model.ClientInbound{
		{ClientId: records[0].Id, InboundId: wg.Id},
		{ClientId: records[1].Id, InboundId: vless.Id},
		{ClientId: records[2].Id, InboundId: naive.Id},
	} {
		if err := db.Create(&link).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, pair := range []struct {
		ib *model.Inbound
		e  string
	}{
		{wg, "wg@example"},
		{vless, "vless@example"},
		{naive, "naive@example"},
	} {
		if err := db.Create(&xray.ClientTraffic{InboundId: pair.ib.Id, Email: pair.e, Enable: true}).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := SyncClientCoreCompatibility(CoreTypeXray, CoreTypeSingBox); err != nil {
		t.Fatal(err)
	}

	check := func(email string, enable bool, autoCore string) {
		var record model.ClientRecord
		if err := db.Where("email = ?", email).First(&record).Error; err != nil {
			t.Fatal(err)
		}
		if record.Enable != enable || record.AutoDisabledByCore != autoCore {
			t.Fatalf("%s: enable=%v auto=%q, want enable=%v auto=%q",
				email, record.Enable, record.AutoDisabledByCore, enable, autoCore)
		}
	}

	check("wg@example", false, CoreTypeSingBox)
	check("vless@example", true, "")
	check("naive@example", true, "")

	if err := SyncClientCoreCompatibility(CoreTypeSingBox, CoreTypeXray); err != nil {
		t.Fatal(err)
	}
	check("wg@example", true, "")
	check("naive@example", false, CoreTypeXray)
}

func TestCoreCompatibilityManualClientEditClearsAutoDisableMarker(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()
	record := &model.ClientRecord{Email: "manual@example", Enable: false, AutoDisabledByCore: CoreTypeSingBox}
	if err := db.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(record).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}

	if err := clearCoreAutoDisabledByEmail(record.Email); err != nil {
		t.Fatal(err)
	}

	var got model.ClientRecord
	if err := db.Where("email = ?", record.Email).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Enable {
		t.Fatal("manual disable state unexpectedly changed")
	}
	if got.AutoDisabledByCore != "" {
		t.Fatalf("auto-disabled marker = %q, want empty", got.AutoDisabledByCore)
	}
}
