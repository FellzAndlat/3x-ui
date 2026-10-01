package controller

import (
	"fmt"
	"strings"
	"sync"

	"github.com/SawaMEN/3x-ui/v3/internal/gateway"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

// GatewayController exposes the transparent gateway controls used by the panel
// UI. The active core decides which config template receives the Gateway
// inbound, while the gateway package owns the core-specific config mutation.
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

func gatewayStateForCore(coreType string) (gateway.State, error) {
	switch coreType {
	case service.CoreTypeXray:
		return gateway.GetState()
	case service.CoreTypeSingBox:
		return gateway.GetSingBoxState()
	default:
		return gateway.State{}, fmt.Errorf("unsupported core type %q", coreType)
	}
}

func enableGatewayForCore(coreType string) error {
	switch coreType {
	case service.CoreTypeXray:
		return gateway.Enable()
	case service.CoreTypeSingBox:
		return gateway.EnableSingBox()
	default:
		return fmt.Errorf("unsupported core type %q", coreType)
	}
}

func disableGatewayForCore(coreType string) error {
	switch coreType {
	case service.CoreTypeXray:
		return gateway.Disable()
	case service.CoreTypeSingBox:
		return gateway.DisableSingBox()
	default:
		return fmt.Errorf("unsupported core type %q", coreType)
	}
}

func otherGatewayCore(coreType string) string {
	if coreType == service.CoreTypeSingBox {
		return service.CoreTypeXray
	}
	return service.CoreTypeSingBox
}

// rollbackGatewayChange restores the two template states after a failed core
// migration. It deliberately operates on config only; the caller decides
// whether the running core also needs to be reconciled afterwards.
func rollbackGatewayChange(currentCore, previousCore string, disableCurrent, restorePrevious bool) error {
	failures := make([]string, 0, 2)
	if disableCurrent {
		if err := disableGatewayForCore(currentCore); err != nil {
			failures = append(failures, fmt.Sprintf("disable %s: %v", currentCore, err))
		}
	}
	if restorePrevious {
		if err := enableGatewayForCore(previousCore); err != nil {
			failures = append(failures, fmt.Sprintf("restore %s: %v", previousCore, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

// restoreGatewayCores rolls back a multi-core disable in reverse order so the
// logical Gateway ownership observed before the request is reconstructed.
func restoreGatewayCores(cores []string) error {
	failures := make([]string, 0, len(cores))
	for i := len(cores) - 1; i >= 0; i-- {
		coreType := cores[i]
		if err := enableGatewayForCore(coreType); err != nil {
			failures = append(failures, fmt.Sprintf("restore %s: %v", coreType, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

type gatewayStatus struct {
	State     gateway.State
	OwnerCore string
	Conflict  bool
}

// gatewayStatusForCore treats Gateway Mode as one logical feature even though
// Xray and sing-box keep independent recovery backups. This is important after
// switching cores: stale Gateway state in the previously selected core must
// remain visible and removable instead of looking disabled.
func gatewayStatusForCore(coreType string) (gatewayStatus, error) {
	activeState, err := gatewayStateForCore(coreType)
	if err != nil {
		return gatewayStatus{}, fmt.Errorf("get %s Gateway state: %w", coreType, err)
	}

	otherCore := otherGatewayCore(coreType)
	otherState, otherErr := gatewayStateForCore(otherCore)
	if otherErr != nil {
		// An unused core may not have a usable template yet. Do not make Gateway
		// status unavailable for the selected core solely because the inactive
		// core cannot be inspected.
		owner := ""
		if activeState.Enabled {
			owner = coreType
		}
		return gatewayStatus{State: activeState, OwnerCore: owner}, nil
	}

	if activeState.Enabled && otherState.Enabled {
		activeState.Configured = activeState.Configured || otherState.Configured
		activeState.BackupExists = activeState.BackupExists || otherState.BackupExists
		return gatewayStatus{
			State:     activeState,
			OwnerCore: "multiple",
			Conflict:  true,
		}, nil
	}
	if activeState.Enabled {
		return gatewayStatus{State: activeState, OwnerCore: coreType}, nil
	}
	if otherState.Enabled {
		return gatewayStatus{State: otherState, OwnerCore: otherCore}, nil
	}
	return gatewayStatus{State: activeState}, nil
}

func (a *GatewayController) statusPayload() (gin.H, error) {
	coreType, coreErr := a.settingService.GetCoreType()
	payload := gin.H{
		"enabled":         false,
		"configured":      false,
		"recoveryBackup":  false,
		"canEnable":       false,
		"coreType":        coreType,
		"gatewayCoreType": "",
		"coreMismatch":    false,
		"conflict":        false,
		"xrayRunning":     a.xrayService.IsXrayRunning(),
		"port":            gateway.InboundPort(),
	}
	if coreErr != nil {
		return payload, coreErr
	}

	status, stateErr := gatewayStatusForCore(coreType)
	payload["enabled"] = status.State.Enabled
	payload["configured"] = status.State.Configured
	payload["recoveryBackup"] = status.State.BackupExists
	payload["gatewayCoreType"] = status.OwnerCore
	payload["conflict"] = status.Conflict
	payload["coreMismatch"] = status.OwnerCore != "" && status.OwnerCore != "multiple" && status.OwnerCore != coreType
	payload["canEnable"] = !status.Conflict && status.OwnerCore != coreType
	if stateErr != nil {
		return payload, stateErr
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

	state, err := gatewayStateForCore(coreType)
	if err != nil {
		payload, _ := a.statusPayload()
		jsonObj(c, payload, err)
		return
	}

	wasRunning := a.xrayService.IsXrayRunning()
	changed := false
	currentEnabledByRequest := false
	previousCoreDisabled := false
	otherCore := otherGatewayCore(coreType)
	if otherState, otherErr := gatewayStateForCore(otherCore); otherErr == nil && otherState.Enabled {
		if err := disableGatewayForCore(otherCore); err != nil {
			payload, _ := a.statusPayload()
			jsonObj(c, payload, fmt.Errorf("disable stale %s Gateway Mode before switching to %s: %w", otherCore, coreType, err))
			return
		}
		previousCoreDisabled = true
		changed = true
	}

	if !state.Enabled {
		if err := enableGatewayForCore(coreType); err != nil {
			if previousCoreDisabled {
				if rollbackErr := rollbackGatewayChange(coreType, otherCore, false, true); rollbackErr != nil {
					err = fmt.Errorf("enable %s Gateway Mode: %w; rollback failed: %v", coreType, err, rollbackErr)
				} else {
					err = fmt.Errorf("enable %s Gateway Mode: %w; previous %s Gateway Mode restored", coreType, err, otherCore)
				}
			}
			payload, _ := a.statusPayload()
			jsonObj(c, payload, err)
			return
		}
		currentEnabledByRequest = true
		changed = true
	}

	// A manually stopped core must stay stopped. Repeated enable requests are
	// idempotent and do not restart an unchanged core process.
	if changed && wasRunning {
		if restartErr := a.xrayService.RestartXray(false); restartErr != nil {
			rollbackErr := rollbackGatewayChange(coreType, otherCore, currentEnabledByRequest, previousCoreDisabled)
			if rollbackErr != nil {
				payload, _ := a.statusPayload()
				jsonObj(c, payload, fmt.Errorf("apply %s Gateway Mode restart failed: %v; rollback failed: %w", coreType, restartErr, rollbackErr))
				return
			}

			// The process was running before this request. Once the templates are
			// restored, reconcile the runtime again so a failed/partial hot reload
			// cannot leave it serving the attempted Gateway configuration.
			if restoreErr := a.xrayService.RestartXray(false); restoreErr != nil {
				payload, _ := a.statusPayload()
				jsonObj(c, payload, fmt.Errorf("apply %s Gateway Mode restart failed: %v; config rollback succeeded but runtime restore failed: %w", coreType, restartErr, restoreErr))
				return
			}

			payload, _ := a.statusPayload()
			jsonObj(c, payload, fmt.Errorf("apply %s Gateway Mode restart failed and the Gateway configuration was rolled back: %w", coreType, restartErr))
			return
		}
	}

	payload, err := a.statusPayload()
	jsonObj(c, payload, err)
}

func (a *GatewayController) disable(c *gin.Context) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()

	coreType, err := a.settingService.GetCoreType()
	if err != nil {
		payload, _ := a.statusPayload()
		jsonObj(c, payload, err)
		return
	}

	wasRunning := a.xrayService.IsXrayRunning()
	disabledCores := make([]string, 0, 2)
	for _, candidate := range []string{coreType, otherGatewayCore(coreType)} {
		state, stateErr := gatewayStateForCore(candidate)
		if stateErr != nil {
			// The inactive core may not have a template yet. The selected core is
			// always expected to be inspectable; surface that failure immediately.
			if candidate == coreType {
				payload, _ := a.statusPayload()
				jsonObj(c, payload, fmt.Errorf("%s Gateway state: %w", candidate, stateErr))
				return
			}
			continue
		}
		if !state.Enabled {
			continue
		}
		if err := disableGatewayForCore(candidate); err != nil {
			rollbackErr := restoreGatewayCores(disabledCores)
			payload, _ := a.statusPayload()
			if rollbackErr != nil {
				jsonObj(c, payload, fmt.Errorf("disable %s Gateway Mode: %v; rollback failed: %w", candidate, err, rollbackErr))
			} else {
				jsonObj(c, payload, fmt.Errorf("disable %s Gateway Mode: %w; earlier changes rolled back", candidate, err))
			}
			return
		}
		disabledCores = append(disabledCores, candidate)
	}

	if len(disabledCores) > 0 && wasRunning {
		if restartErr := a.xrayService.RestartXray(false); restartErr != nil {
			rollbackErr := restoreGatewayCores(disabledCores)
			if rollbackErr != nil {
				payload, _ := a.statusPayload()
				jsonObj(c, payload, fmt.Errorf("disable Gateway Mode restart failed: %v; rollback failed: %w", restartErr, rollbackErr))
				return
			}

			if restoreErr := a.xrayService.RestartXray(false); restoreErr != nil {
				payload, _ := a.statusPayload()
				jsonObj(c, payload, fmt.Errorf("disable Gateway Mode restart failed: %v; config rollback succeeded but runtime restore failed: %w", restartErr, restoreErr))
				return
			}

			payload, _ := a.statusPayload()
			jsonObj(c, payload, fmt.Errorf("disable Gateway Mode restart failed and the Gateway configuration was rolled back: %w", restartErr))
			return
		}
	}

	payload, err := a.statusPayload()
	jsonObj(c, payload, err)
}
