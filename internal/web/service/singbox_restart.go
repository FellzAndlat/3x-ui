package service

import (
	"context"
	"sync/atomic"

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

var isNeedSingBoxRestart atomic.Bool

// SetToNeedRestart marks the selected sing-box runtime config as stale.
// Inbound/client controllers use the same deferred-restart model as Xray
// so a burst of node-sync mutations is collapsed into one core reload.
func (s *SingBoxService) SetToNeedRestart() {
	isNeedSingBoxRestart.Store(true)
}

// IsNeedRestartAndSetFalse consumes the pending sing-box restart flag.
func (s *SingBoxService) IsNeedRestartAndSetFalse() bool {
	return isNeedSingBoxRestart.CompareAndSwap(true, false)
}

// ApplyPendingRestart reloads sing-box only while it is the selected,
// currently running core. If sing-box is absent/stopped there is no
// process to reload; a later explicit start writes the current config.
func (s *SingBoxService) ApplyPendingRestart(ctx context.Context) {
	if !s.IsNeedRestartAndSetFalse() {
		return
	}
	coreType, err := (&SettingService{}).GetCoreType()
	if err != nil {
		logger.Error("get selected core for pending sing-box restart failed:", err)
		s.SetToNeedRestart()
		return
	}
	if coreType != CoreTypeSingBox || !s.IsRunning() {
		return
	}
	if err := s.Restart(ctx); err != nil {
		logger.Error("restart sing-box failed:", err)
		s.SetToNeedRestart()
	}
}
