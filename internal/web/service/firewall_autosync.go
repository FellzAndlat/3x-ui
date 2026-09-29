package service

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

const (
	firewallControlInitializedKey = "firewallControlInitialized"
	firewallSafetyPortKey         = "firewallSafetyPort"
)

var firewallAutoSyncOnce sync.Once

func (s *FirewallService) MarkControlInitialized() error {
	return (&SettingService{}).setBool(firewallControlInitializedKey, true)
}

// RememberSafetyPort persists the externally visible panel port discovered on
// an authenticated firewall-management request. Do not replace a previously
// learned external reverse-proxy port with the panel's internal listen port:
// doing so would let the next background reconcile close the public entrypoint.
func (s *FirewallService) RememberSafetyPort(port int) error {
	if port < 1 || port > 65535 {
		return nil
	}
	settings := &SettingService{}
	panelPort, _ := settings.GetPort()
	previous := rememberedFirewallSafetyPort()
	if previous > 0 && previous != panelPort && port == panelPort {
		return nil
	}
	return settings.setInt(firewallSafetyPortKey, port)
}

func rememberedFirewallSafetyPort() int {
	raw, err := firewallSetting(firewallSafetyPortKey, "0")
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}

func firewallControlInitialized() bool {
	raw, err := firewallSetting(firewallControlInitializedKey, "false")
	if err != nil {
		return false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	enabled, err := strconv.ParseBool(raw)
	return err == nil && enabled
}

// SetAutoSyncPreference changes automatic inbound reconciliation. Turning it
// off deliberately freezes the rules currently present instead of immediately
// removing every 3x-ui-owned inbound rule; the operator may still use Sync now
// or manual rules while automatic reconciliation is disabled.
func (s *FirewallService) SetAutoSyncPreference(ctx context.Context, enabled bool, safetyPort int) (FirewallStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()

	settings := &SettingService{}
	if !enabled {
		if err := settings.setBool(firewallAutoSyncKey, false); err != nil {
			return FirewallStatus{}, err
		}
		return s.status(ctx, safetyPort)
	}

	backend, err := detectFirewallBackend(ctx)
	if err != nil {
		return FirewallStatus{}, err
	}
	if err := settings.setBool(firewallAutoSyncKey, true); err != nil {
		return FirewallStatus{}, err
	}
	if err := s.sync(ctx, backend, safetyPort); err != nil {
		return FirewallStatus{}, err
	}
	return s.status(ctx, safetyPort)
}

// StartAutoSync keeps firewall rules aligned with enabled local inbounds even
// when changes arrive through imports, API calls, or node synchronization.
// Inbound API mutations trigger an immediate coalesced reconcile; a periodic
// five-second pass remains as drift repair for imports and future mutation
// paths and keeps the managed iptables jump behind newly added admin rules.
func (s *FirewallService) StartAutoSync() {
	firewallAutoSyncOnce.Do(func() {
		go func() {
			syncNow := func() {
				if !firewallControlInitialized() {
					return
				}
				auto, err := firewallAutoSync()
				if err != nil || !auto {
					return
				}

				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()

				firewallMu.Lock()
				defer firewallMu.Unlock()

				if err := migrateManagedBackendIfNeededLocked(ctx); err != nil {
					logger.Debug("firewall backend migration failed:", err)
					return
				}
				backend, err := detectManagedFirewallBackend(ctx)
				if err != nil {
					return
				}
				on, err := backend.enabled(ctx)
				if err != nil || !on {
					return
				}
				if err := s.syncManagedSafeLocked(ctx, backend, rememberedFirewallSafetyPort()); err != nil {
					logger.Debug("firewall auto-sync failed:", err)
				}
			}

			syncNow()
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
				case <-firewallSyncTrigger:
				}
				syncNow()
			}
		}()
	})
}
