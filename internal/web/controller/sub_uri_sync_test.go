package controller

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

func TestNormalizeBoundRequestResetsGeneratedSubscriptionURIsAfterPortChange(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	settingService := &service.SettingService{}
	current, err := settingService.GetAllSetting()
	if err != nil {
		t.Fatalf("GetAllSetting: %v", err)
	}
	current.SubDomain = "sub.example.com"
	current.SubPort = 2096
	current.SubURI = "http://sub.example.com:2096/sub/"
	current.SubJsonURI = "http://sub.example.com:2096/json/"
	current.SubClashURI = "http://sub.example.com:2096/clash/"
	if err := settingService.UpdateAllSetting(current, service.SecretClears{}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	next := *current
	next.SubPort = 8443
	form := &updateSettingForm{AllSetting: next}
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "http://panel.example.com/panel/api/setting/update", nil)

	form.NormalizeBoundRequest(ctx)

	if form.SubURI != "" {
		t.Fatalf("generated subURI was not cleared: %q", form.SubURI)
	}
	if form.SubJsonURI != "" {
		t.Fatalf("generated subJsonURI was not cleared: %q", form.SubJsonURI)
	}
	if form.SubClashURI != "" {
		t.Fatalf("generated subClashURI was not cleared: %q", form.SubClashURI)
	}
}

func TestNormalizeBoundRequestPreservesCustomReverseProxyURI(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	settingService := &service.SettingService{}
	current, err := settingService.GetAllSetting()
	if err != nil {
		t.Fatalf("GetAllSetting: %v", err)
	}
	current.SubDomain = "sub.example.com"
	current.SubPort = 2096
	current.SubURI = "https://proxy.example.com/custom-sub/"
	if err := settingService.UpdateAllSetting(current, service.SecretClears{}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	next := *current
	next.SubPort = 8443
	form := &updateSettingForm{AllSetting: next}
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "http://panel.example.com/panel/api/setting/update", nil)

	form.NormalizeBoundRequest(ctx)

	if form.SubURI != current.SubURI {
		t.Fatalf("custom reverse-proxy subURI changed: got %q want %q", form.SubURI, current.SubURI)
	}
}

func TestNormalizeBoundRequestRecognizesGeneratedStandardPortURI(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	settingService := &service.SettingService{}
	current, err := settingService.GetAllSetting()
	if err != nil {
		t.Fatalf("GetAllSetting: %v", err)
	}
	current.SubDomain = "sub.example.com"
	current.SubPort = 80
	current.SubURI = "http://sub.example.com/sub/"
	if err := settingService.UpdateAllSetting(current, service.SecretClears{}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	next := *current
	next.SubPort = 2096
	form := &updateSettingForm{AllSetting: next}
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "http://panel.example.com/panel/api/setting/update", nil)

	form.NormalizeBoundRequest(ctx)

	if form.SubURI != "" {
		t.Fatalf("generated standard-port subURI was not cleared: %q", form.SubURI)
	}
}
