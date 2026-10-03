package controller

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

func TestAdBlockSchedulerRespectsRetryAndAutomaticUpdate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dir)
	if err := database.InitDB(filepath.Join(dir, "x-ui.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	a := &AdBlockController{}
	interval := 1
	if err := a.settingService.SaveAdBlockSettings(context.Background(), service.AdBlockSettings{Enabled: true, CustomDomains: "ads.example", UpdateIntervalHours: &interval}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(2 * time.Hour)
	due, err := a.adBlockUpdateDue(now)
	if err != nil || !due {
		t.Fatalf("refresh not due: %v %v", due, err)
	}
	retry := now.Add(5 * time.Minute)
	if err := database.GetDB().Model(&model.Setting{}).Where("key = ?", "adBlockNextRetry").Update("value", retry.Format(time.RFC3339)).Error; err != nil {
		t.Fatal(err)
	}
	due, err = a.adBlockUpdateDue(now)
	if err != nil || due {
		t.Fatal("scheduler ignored retry delay")
	}
	due, err = a.adBlockUpdateDue(retry)
	if err != nil || !due {
		t.Fatal("scheduler ignored retry deadline")
	}
	if err := a.settingService.SetAdBlockAutoUpdate(false); err != nil {
		t.Fatal(err)
	}
	due, err = a.adBlockUpdateDue(retry)
	if err != nil || due {
		t.Fatal("scheduler ignored disabled automatic updates")
	}
	if err := a.settingService.SetAdBlockAutoUpdate(true); err != nil {
		t.Fatal(err)
	}
	if err := a.settingService.SetAdBlockEnable(false); err != nil {
		t.Fatal(err)
	}
	due, err = a.adBlockUpdateDue(retry)
	if err != nil || due {
		t.Fatal("scheduler ignored disabled filtering")
	}
}
