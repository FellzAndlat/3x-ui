package controller

import (
	"context"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

const (
	adBlockSchedulerInitialDelay = 5 * time.Second
	adBlockSchedulerCheckEvery   = 15 * time.Second
	adBlockSchedulerTimeout      = 5 * time.Minute
)

var adBlockSchedulerOnce sync.Once

func startAdBlockScheduler(a *AdBlockController) {
	if a == nil {
		return
	}
	adBlockSchedulerOnce.Do(func() {
		go a.runAdBlockScheduler()
	})
}

func (a *AdBlockController) runAdBlockScheduler() {
	initial := time.NewTimer(adBlockSchedulerInitialDelay)
	defer initial.Stop()

	<-initial.C
	a.runScheduledAdBlockUpdate()

	ticker := time.NewTicker(adBlockSchedulerCheckEvery)
	defer ticker.Stop()
	for range ticker.C {
		a.runScheduledAdBlockUpdate()
	}
}

func (a *AdBlockController) runScheduledAdBlockUpdate() {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()

	if err := a.settingService.ReconcileAdBlockApplication(); err != nil {
		logger.Warningf("AdBlock reconcile failed: %v", err)
		return
	}
	state, err := a.settingService.GetAdBlockApplication()
	if err != nil {
		logger.Warningf("AdBlock apply status failed: %v", err)
		return
	}
	if state.Pending {
		retry, _ := time.Parse(time.RFC3339, state.NextRetry)
		if retry.After(time.Now()) && retry.Before(time.Now().Add(10*time.Minute)) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), adBlockSchedulerTimeout)
		err := a.applyPending(ctx)
		cancel()
		if err != nil {
			logger.Warningf("AdBlock pending application failed: %v", err)
			return
		}
	}
	due, err := a.adBlockUpdateDue(time.Now().UTC())
	if err != nil {
		logger.Warningf("AdBlock automatic update skipped: %v", err)
		return
	}
	if !due {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), adBlockSchedulerTimeout)
	defer cancel()

	result, err := a.settingService.UpdateAdBlock(ctx)
	if err != nil {
		logger.Warningf("AdBlock automatic update failed; keeping last-known-good list: %v", err)
		return
	}

	if err := a.applyPending(ctx); err != nil {
		logger.Errorf("AdBlock automatic application failed: %v", err)
		return
	}

	logger.Infof(
		"AdBlock automatic update complete: domains=%d sources=%d changed=%t",
		result.DomainCount,
		result.SourceCount,
		result.Changed,
	)
}

func (a *AdBlockController) adBlockUpdateDue(now time.Time) (bool, error) {
	enabled, err := a.settingService.GetAdBlockEnable()
	if err != nil {
		return false, err
	}
	if !enabled {
		return false, nil
	}

	autoUpdate, err := a.settingService.GetAdBlockAutoUpdate()
	if err != nil {
		return false, err
	}
	if !autoUpdate {
		return false, nil
	}

	retryDue, err := a.settingService.AdBlockRetryDue(now)
	if err != nil || !retryDue {
		return false, err
	}

	retry, err := a.settingService.GetAdBlockAutomationStatus()
	if err != nil {
		return false, err
	}
	if retry.RetryCount > 0 {
		return true, nil
	}
	intervalHours, err := a.settingService.GetAdBlockUpdateIntervalHours()
	if err != nil {
		return false, err
	}
	lastUpdateRaw, err := a.settingService.GetAdBlockLastUpdate()
	if err != nil {
		return false, err
	}
	if lastUpdateRaw == "" {
		return true, nil
	}

	lastUpdate, err := time.Parse(time.RFC3339, lastUpdateRaw)
	if err != nil {
		logger.Warningf("AdBlock last update timestamp %q is invalid; refreshing list", lastUpdateRaw)
		return true, nil
	}

	// A timestamp significantly in the future usually means the system clock
	// moved backwards. Refresh instead of suppressing automatic updates for an
	// unbounded amount of time.
	if lastUpdate.After(now.Add(5 * time.Minute)) {
		return true, nil
	}

	return now.Sub(lastUpdate) >= time.Duration(intervalHours)*time.Hour, nil
}
