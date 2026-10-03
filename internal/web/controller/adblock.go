package controller

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
	"github.com/SawaMEN/3x-ui/v3/internal/adblock/youtube"
	"github.com/SawaMEN/3x-ui/v3/internal/adblock/youtubeproxy"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
	"github.com/gin-gonic/gin"
)

const maxAdBlockSettingsBody = 4 << 20

type AdBlockController struct {
	settingService service.SettingService
	xrayService    service.XrayService
	singBoxService service.SingBoxService
	operationMu    sync.Mutex
}

type adBlockSettingsForm struct {
	Server              *service.AdBlockServerSettings `json:"server"`
	Policies            *[]service.AdBlockPolicy       `json:"policies"`
	Scope               *service.AdBlockScope          `json:"scope"`
	Enabled             bool                           `json:"enabled"`
	Sources             string                         `json:"sources"`
	CustomDomains       string                         `json:"customDomains"`
	Allowlist           string                         `json:"allowlist"`
	AutoUpdate          *bool                          `json:"autoUpdate"`
	UpdateIntervalHours *int                           `json:"updateIntervalHours"`
	Profile             *string                        `json:"profile"`
	YoutubeMode         *string                        `json:"youtubeMode"`
}

type adBlockStatus struct {
	ServerOutbounds          []string                         `json:"serverOutbounds"`
	Server                   service.AdBlockServerSettings    `json:"server"`
	ServerRuntime            youtubeproxy.Counters            `json:"serverRuntime"`
	ServerCertificate        youtubeproxy.CertificateInfo     `json:"serverCertificate"`
	Policies                 []service.AdBlockPolicy          `json:"policies"`
	Scope                    service.AdBlockScope             `json:"scope"`
	ScopeOptions             *service.AdBlockScopeOptions     `json:"scopeOptions"`
	Application              *service.AdBlockApplication      `json:"application"`
	PausedUntil              string                           `json:"pausedUntil"`
	SourceStatuses           []adblock.SourceStatus           `json:"sourceStatuses"`
	Enabled                  bool                             `json:"enabled"`
	Sources                  string                           `json:"sources"`
	CustomDomains            string                           `json:"customDomains"`
	Allowlist                string                           `json:"allowlist"`
	AutoUpdate               bool                             `json:"autoUpdate"`
	UpdateIntervalHours      int                              `json:"updateIntervalHours"`
	LastUpdate               string                           `json:"lastUpdate"`
	DomainCount              int                              `json:"domainCount"`
	SourceCount              int                              `json:"sourceCount"`
	Profile                  string                           `json:"profile"`
	Profiles                 []service.AdBlockProfile         `json:"profiles"`
	YoutubeMode              string                           `json:"youtubeMode"`
	YoutubeVideoAdsSupported bool                             `json:"youtubeVideoAdsSupported"`
	Automation               *service.AdBlockAutomationStatus `json:"automation"`
}

func NewAdBlockController(g *gin.RouterGroup) *AdBlockController {
	a := &AdBlockController{}
	group := g.Group("/adblock")
	group.GET("/status", a.status)
	group.POST("/settings", a.saveSettings)
	group.POST("/update", a.update)
	group.POST("/apply", a.apply)
	group.POST("/pause", a.pause)
	group.POST("/check", a.checkDomain)
	group.GET("/youtube-extension", a.youtubeExtension)
	group.GET("/youtube-server-certificate", a.youtubeServerCertificate)
	group.GET("/youtube-server-routing/:core", a.youtubeServerRouting)
	startAdBlockScheduler(a)
	return a
}

func (a *AdBlockController) readStatus() (*adBlockStatus, error) {
	outboundOptions, _ := a.settingService.AdBlockServerOutboundOptions()
	server, err := a.settingService.GetAdBlockServer()
	if err != nil {
		return nil, err
	}
	certificate := youtubeproxy.CertificateInfo{}
	if _, err := os.Stat(filepath.Join(service.AdBlockServerCADir(), "ca-private.pem")); err == nil {
		_, certificate, _ = youtubeproxy.Certificate(service.AdBlockServerCADir())
	}
	enabled, err := a.settingService.GetAdBlockEnable()
	if err != nil {
		return nil, err
	}
	sources, err := a.settingService.GetAdBlockSources()
	if err != nil {
		return nil, err
	}
	customDomains, err := a.settingService.GetAdBlockCustomDomains()
	if err != nil {
		return nil, err
	}
	allowlist, err := a.settingService.GetAdBlockAllowlist()
	if err != nil {
		return nil, err
	}
	autoUpdate, err := a.settingService.GetAdBlockAutoUpdate()
	if err != nil {
		return nil, err
	}
	updateIntervalHours, err := a.settingService.GetAdBlockUpdateIntervalHours()
	if err != nil {
		return nil, err
	}
	lastUpdate, err := a.settingService.GetAdBlockLastUpdate()
	if err != nil {
		return nil, err
	}
	domainCount, err := a.settingService.GetAdBlockDomainCount()
	if err != nil {
		return nil, err
	}
	sourceCount, err := a.settingService.GetAdBlockSourceCount()
	if err != nil {
		return nil, err
	}
	profile, err := a.settingService.GetAdBlockProfile()
	if err != nil {
		return nil, err
	}
	mode, err := a.settingService.GetAdBlockYoutubeMode()
	if err != nil {
		return nil, err
	}
	automation, err := a.settingService.GetAdBlockAutomationStatus()
	if err != nil {
		return nil, err
	}

	scope, err := a.settingService.GetAdBlockScope()
	if err != nil {
		return nil, err
	}
	options, err := a.settingService.GetAdBlockScopeOptions()
	if err != nil {
		return nil, err
	}
	application, err := a.settingService.GetAdBlockApplication()
	if err != nil {
		return nil, err
	}
	pausedUntil, err := a.settingService.GetAdBlockPausedUntil()
	if err != nil {
		return nil, err
	}
	sourcesStatus, err := a.settingService.GetAdBlockSourceStatuses()
	if err != nil {
		return nil, err
	}
	policies, err := a.settingService.GetAdBlockPolicies()
	if err != nil {
		return nil, err
	}
	return &adBlockStatus{ServerOutbounds: outboundOptions, Server: server, ServerRuntime: youtubeproxy.Status(), ServerCertificate: certificate,
		Policies: policies,
		Enabled:  enabled,
		Scope:    scope, ScopeOptions: options, Application: application, PausedUntil: pausedUntil, SourceStatuses: sourcesStatus,
		Sources:             sources,
		CustomDomains:       customDomains,
		Allowlist:           allowlist,
		AutoUpdate:          autoUpdate,
		UpdateIntervalHours: updateIntervalHours,
		LastUpdate:          lastUpdate,
		DomainCount:         domainCount,
		SourceCount:         sourceCount,
		Profile:             profile, Profiles: service.AdBlockProfiles(), YoutubeMode: mode, Automation: automation, YoutubeVideoAdsSupported: false,
	}, nil
}

func (a *AdBlockController) status(c *gin.Context) {
	status, err := a.readStatus()
	if err != nil {
		jsonMsg(c, "failed to read AdBlock settings", err)
		return
	}
	jsonObj(c, status, nil)
}

func (a *AdBlockController) saveSettings(c *gin.Context) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAdBlockSettingsBody)
	var form adBlockSettingsForm
	if err := c.ShouldBindJSON(&form); err != nil {
		jsonMsg(c, "invalid AdBlock settings", err)
		return
	}

	before, err := a.settingService.AdBlockSnapshotForRollback()
	if err != nil {
		jsonMsg(c, "failed to snapshot settings", err)
		return
	}
	if err := a.settingService.SaveAdBlockSettings(c.Request.Context(), service.AdBlockSettings{
		Server: form.Server, Policies: form.Policies, Scope: form.Scope, Enabled: form.Enabled, Sources: form.Sources, CustomDomains: form.CustomDomains,
		Allowlist: form.Allowlist, AutoUpdate: form.AutoUpdate, UpdateIntervalHours: form.UpdateIntervalHours,
		Profile: form.Profile, YoutubeMode: form.YoutubeMode,
	}); err != nil {
		jsonMsg(c, "failed to save AdBlock settings; previous settings preserved", err)
		return
	}

	if err := a.applyPending(c.Request.Context()); err != nil {
		youtubeproxy.Abort()
		rollbackErr := a.settingService.RestoreAdBlockSnapshot(before)
		if rollbackErr == nil {
			rollbackErr = restartSelectedCoreNow(c.Request.Context(), &a.settingService, &a.xrayService, &a.singBoxService)
			if rollbackErr == nil {
				rollbackErr = a.settingService.MarkAdBlockApplied(nil)
			}
		}
		if rollbackErr != nil {
			jsonMsg(c, "application and rollback failed", fmt.Errorf("apply: %v; rollback: %w", err, rollbackErr))
			return
		}
		jsonMsg(c, "settings rolled back after core application failed", err)
		return
	}

	status, err := a.readStatus()
	if err != nil {
		jsonMsg(c, "AdBlock settings saved, but status refresh failed", err)
		return
	}
	jsonObj(c, status, nil)
}

func (a *AdBlockController) update(c *gin.Context) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()

	result, err := a.settingService.UpdateAdBlock(c.Request.Context())
	if err != nil {
		jsonMsg(c, "failed to update AdBlock lists", err)
		return
	}

	if err := a.applyPending(c.Request.Context()); err != nil {
		jsonMsg(c, "AdBlock lists updated, but core restart failed", err)
		return
	}

	status, err := a.readStatus()
	if err != nil {
		jsonMsg(c, "AdBlock lists updated, but status refresh failed", err)
		return
	}
	jsonObj(c, gin.H{
		"status": status,
		"update": result,
	}, nil)
}

func (a *AdBlockController) applyPending(ctx context.Context) error {
	state, err := a.settingService.GetAdBlockApplication()
	if err != nil {
		return err
	}
	if !state.Pending {
		return nil
	}
	server, readErr := a.settingService.GetAdBlockServer()
	if readErr != nil {
		return readErr
	}
	if server.Enabled || youtubeproxy.Status().Running {
		if err := a.validateServerCore(ctx); err != nil {
			youtubeproxy.Abort()
			_ = a.settingService.MarkAdBlockApplied(err)
			return err
		}
	}
	err = restartSelectedCoreNow(ctx, &a.settingService, &a.xrayService, &a.singBoxService)
	if recordErr := a.settingService.MarkAdBlockApplied(err); recordErr != nil {
		return recordErr
	}
	return err
}
func (a *AdBlockController) apply(c *gin.Context) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if err := a.settingService.ReconcileAdBlockApplication(); err != nil {
		jsonMsg(c, "failed to read application state", err)
		return
	}
	if err := a.applyPending(c.Request.Context()); err != nil {
		jsonMsg(c, "core restart failed", err)
		return
	}
	status, err := a.readStatus()
	jsonObj(c, status, err)
}
func (a *AdBlockController) pause(c *gin.Context) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var form struct {
		Minutes *int `json:"minutes"`
	}
	if err := c.ShouldBindJSON(&form); err != nil || form.Minutes == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "minutes is required"})
		return
	}
	if err := a.settingService.SetAdBlockPause(*form.Minutes); err != nil {
		jsonMsg(c, "invalid pause", err)
		return
	}
	if err := a.applyPending(c.Request.Context()); err != nil {
		jsonMsg(c, "pause saved, but core restart failed", err)
		return
	}
	status, err := a.readStatus()
	jsonObj(c, status, err)
}
func (a *AdBlockController) checkDomain(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var form struct {
		Domain  string `json:"domain"`
		Inbound string `json:"inbound"`
		Client  string `json:"client"`
	}
	if err := c.ShouldBindJSON(&form); err != nil {
		jsonMsg(c, "invalid domain check", err)
		return
	}
	result, err := a.settingService.CheckAdBlockDomain(form.Domain, form.Inbound, form.Client)
	jsonObj(c, result, err)
}

func (a *AdBlockController) youtubeExtension(c *gin.Context) {
	data, err := youtube.Archive()
	if err != nil {
		jsonMsg(c, "failed to create YouTube companion archive", err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="3x-ui-youtube-companion.zip"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/zip", data)
}

func (a *AdBlockController) youtubeServerRouting(c *gin.Context) {
	data, err := youtubeproxy.RoutingPreset(c.Param("core"), "127.0.0.1:18080")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="youtube-server-routing.json"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/json", data)
}

func (a *AdBlockController) youtubeServerCertificate(c *gin.Context) {
	data, _, err := youtubeproxy.Certificate(service.AdBlockServerCADir())
	if err != nil {
		jsonMsg(c, "failed to create public certificate", err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="3x-ui-youtube-ca.pem"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/x-pem-file", data)
}
