package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
	"github.com/gin-gonic/gin"
)

func setupAdBlockControllerDB(t *testing.T) *AdBlockController {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dir)
	if err := database.InitDB(filepath.Join(dir, "x-ui.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	return &AdBlockController{}
}
func TestAdBlockIntervalSaveDoesNotRestartCore(t *testing.T) {
	a := setupAdBlockControllerDB(t)
	if err := a.settingService.SaveAdBlockSettings(context.Background(), service.AdBlockSettings{Enabled: true, CustomDomains: "ads.example"}); err != nil {
		t.Fatal(err)
	}
	if err := a.settingService.MarkAdBlockApplied(nil); err != nil {
		t.Fatal(err)
	}
	before, _ := a.settingService.GetAdBlockApplication()
	router := gin.New()
	router.POST("/settings", a.saveSettings)
	request := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(`{"enabled":true,"sources":"","customDomains":"ads.example","allowlist":"","autoUpdate":true,"updateIntervalHours":12}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var result struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	// No installed core is needed for an interval-only change.
	if response.Code != http.StatusOK || !result.Success {
		t.Fatalf("interval save attempted core restart: %s", response.Body)
	}
	after, _ := a.settingService.GetAdBlockApplication()
	if after.Pending || after.AppliedAt != before.AppliedAt {
		t.Fatal("interval save changed apply state")
	}
}
func TestAdBlockPartialRefreshRetriesBeforeNormalInterval(t *testing.T) {
	a := setupAdBlockControllerDB(t)
	if err := a.settingService.SaveAdBlockSettings(context.Background(), service.AdBlockSettings{Enabled: true, CustomDomains: "ads.example"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for key, value := range map[string]string{"adBlockRetryCount": "1", "adBlockNextRetry": now.Add(time.Minute).Format(time.RFC3339)} {
		if err := database.GetDB().Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.GetDB().Model(&model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}
	due, err := a.adBlockUpdateDue(now)
	if err != nil || due {
		t.Fatal("partial update ignored retry delay")
	}
	due, err = a.adBlockUpdateDue(now.Add(2 * time.Minute))
	if err != nil || !due {
		t.Fatal("partial update waited for daily interval instead of retrying failed source")
	}
}
