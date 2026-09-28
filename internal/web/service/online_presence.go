package service

import "github.com/SawaMEN/3x-ui/v3/internal/xray"

var onlinePresenceFallback = xray.NewProcess(&xray.Config{})

// EnsureOnlinePresenceTracker keeps local/node presence available when sing-box
// is the selected core and no Xray process object exists to carry that state.
func EnsureOnlinePresenceTracker() {
	if currentXrayProcess() != nil {
		return
	}
	xrayState.mu.Lock()
	if xrayState.process == nil {
		xrayState.process = onlinePresenceFallback
	}
	xrayState.mu.Unlock()
}
