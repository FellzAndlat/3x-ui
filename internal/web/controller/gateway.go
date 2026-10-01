package controller

import (
	"errors"
	"fmt"
	"sync"

	"github.com/SawaMEN/3x-ui/v3/internal/gateway"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

// GatewayController exposes the transparent Xray gateway controls used by the
// panel UI. The underlying gateway package remains the single implementation
// shared with the `x-ui gateway` CLI commands.
type GatewayController struct {
	settingService service.SettingService
	xrayService    service.XrayService
	operationMu    sync.Mutex
}

func NewGatewayController(g *gin.RouterGroup) *GatewayController {
	a := &GatewayController{}
	a.initRouter(g)
	return a
}

func (a *GatewayController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/gateway")
	g.GET("/status", a.status)
	g.POST("/enable", a.enable)
	g.POST("/disable", a.disable)
}

func (a *GatewayController) statusPayload() (gin.H, error) {
	state, stateErr := gateway.GetState()
	coreType, coreErr := a.settingService.GetCoreType()

	payload := gin.H{
		"enabled":        state.Enabled,
		"configured":     state.Configured,
		"recoveryBackup": state.BackupExists,
		"canEnable":      !state.Enabled && coreType != service.CoreTypeSingBox,
		"coreType":       coreType,
		"xrayRunning":    a.xrayService.IsXrayRunning(),
		"port":           gateway.InboundPort(),
	}
	if stateErr != nil {
		return payload, stateErr
	}
	if coreErr != nil {
		return payload, coreErr
	}
	return payload, nil
}

func (a *GatewayController) status(c *gin.Context) {
	payload, err := a.statusPayload()
	jsonObj(c, payload, err)
}

func (a *GatewayController) enable(c *gin.Context) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()

	coreType, err := a.settingService.GetCoreType()
	if err != nil {
		payload, _ := a.statusPayload()
		jsonObj(c, payload, err)
		return
	}
	if coreType == service.CoreTypeSingBox {
		payload, _ := a.statusPayload()
		jsonObj(c, payload, errors.New("Gateway Mode is available only when Xray is selected"))
		return
	}

	state, err := gateway.GetState()
	if err != nil {
		payload, _ := a.statusPayload()
		jsonObj(c, payload, err)
		return
	}

	changed := false
	if !state.Enabled {
		if err := gateway.Enable(); err != nil {
			payload, _ := a.statusPayload()
			jsonObj(c, payload, err)
			return
		}
		changed = true
	}

	// A manually stopped core must stay stopped. Repeated enable requests are
	// idempotent and do not restart an unchanged Xray process.
	if changed && a.xrayService.IsXrayRunning() {
		if err := a.xrayService.RestartXray(false); err != nil {
			payload, _ := a.statusPayload()
			jsonObj(c, payload, fmt.Errorf("Gateway Mode was enabled, but Xray restart failed: %w", err))
			return
		}
	}

	payload, err := a.statusPayload()
	jsonObj(c, payload, err)
}

func (a *GatewayController) disable(c *gin.Context) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()

	coreType, coreErr := a.settingService.GetCoreType()
	if coreErr != nil {
		payload, _ := a.statusPayload()
		jsonObj(c, payload, coreErr)
		return
	}

	state, err := gateway.GetState()
	if err != nil {
		payload, _ := a.statusPayload()
		jsonObj(c, payload, err)
		return
	}

	changed := false
	if state.Enabled {
		if err := gateway.Disable(); err != nil {
			payload, _ := a.statusPayload()
			jsonObj(c, payload, err)
			return
		}
		changed = true
	}

	// Disabling remains available after switching to sing-box so stale Gateway
	// config/backup state can always be cleaned up. Restart Xray only when it is
	// the selected running core.
	if changed && coreType != service.CoreTypeSingBox && a.xrayService.IsXrayRunning() {
		if err := a.xrayService.RestartXray(false); err != nil {
			payload, _ := a.statusPayload()
			jsonObj(c, payload, fmt.Errorf("Gateway Mode was disabled, but Xray restart failed: %w", err))
			return
		}
	}

	payload, err := a.statusPayload()
	jsonObj(c, payload, err)
}
