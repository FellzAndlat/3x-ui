package controller

import (
	"context"
	"fmt"

	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

// markSelectedCoreNeedRestart routes a config-change notification to the
// core that this panel currently runs. This is especially important on a
// node: master sync writes local inbounds/clients, so marking Xray while
// the node actually runs sing-box leaves the live sing-box config stale.
func markSelectedCoreNeedRestart(settings *service.SettingService, xrayService *service.XrayService, singBoxService *service.SingBoxService) {
	coreType, err := settings.GetCoreType()
	if err == nil && coreType == service.CoreTypeSingBox {
		singBoxService.SetToNeedRestart()
		return
	}
	xrayService.SetToNeedRestart()
}

// restartSelectedCoreNow regenerates and restarts the selected runtime before
// a caller immediately depends on the new configuration. Node OutboundTag
// changes need this stronger form: the following reachability probe must use
// the newly created loopback egress bridge, not wait for the deferred cron.
func restartSelectedCoreNow(ctx context.Context, settings *service.SettingService, xrayService *service.XrayService, singBoxService *service.SingBoxService) error {
	coreType, err := settings.GetCoreType()
	if err != nil {
		return err
	}
	if coreType == service.CoreTypeSingBox {
		if !singBoxService.Installed() {
			return fmt.Errorf("sing-box is selected but not installed")
		}
		return singBoxService.Restart(ctx)
	}
	return xrayService.RestartXray(false)
}
