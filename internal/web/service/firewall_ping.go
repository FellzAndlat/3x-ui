package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
)

const firewallPingEnabledKey = "firewallPingEnabled"

var firewallPingSysctlPaths = []string{
	"/proc/sys/net/ipv4/icmp_echo_ignore_all",
	"/proc/sys/net/ipv6/icmp/echo_ignore_all",
}

type firewallPingSysctlState struct {
	path  string
	value string
}

func readFirewallPingSysctls() ([]firewallPingSysctlState, error) {
	states := make([]firewallPingSysctlState, 0, len(firewallPingSysctlPaths))
	for _, path := range firewallPingSysctlPaths {
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read ping state %s: %w", path, err)
		}
		value := strings.TrimSpace(string(raw))
		if value != "0" && value != "1" {
			return nil, fmt.Errorf("invalid ping state in %s", path)
		}
		states = append(states, firewallPingSysctlState{path: path, value: value})
	}
	return states, nil
}

func readFirewallPingEnabled() (bool, error) {
	states, err := readFirewallPingSysctls()
	if err != nil {
		return false, err
	}
	for _, state := range states {
		if state.value == "1" {
			return false, nil
		}
	}
	return true, nil
}

func writeFirewallPingSysctl(path, value string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(value + "\n")
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func writeFirewallPingEnabled(enabled bool) error {
	desired := "1"
	if enabled {
		desired = "0"
	}
	states, err := readFirewallPingSysctls()
	if err != nil {
		return err
	}
	if len(states) == 0 {
		return errors.New("ICMP echo sysctl is unavailable")
	}

	changed := make([]firewallPingSysctlState, 0, len(states))
	for _, state := range states {
		if state.value == desired {
			continue
		}
		if err := writeFirewallPingSysctl(state.path, desired); err != nil {
			var rollbackErrs []error
			for i := len(changed) - 1; i >= 0; i-- {
				if rollbackErr := writeFirewallPingSysctl(changed[i].path, changed[i].value); rollbackErr != nil {
					rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback %s: %w", changed[i].path, rollbackErr))
				}
			}
			return errors.Join(append([]error{fmt.Errorf("write ping state %s: %w", state.path, err)}, rollbackErrs...)...)
		}
		changed = append(changed, state)
	}
	return nil
}

func firewallPingPreference() (enabled, configured bool, err error) {
	setting, err := (&SettingService{}).getSetting(firewallPingEnabledKey)
	if database.IsNotFound(err) {
		current, readErr := readFirewallPingEnabled()
		return current, false, readErr
	}
	if err != nil {
		return false, false, err
	}
	raw := strings.TrimSpace(setting.Value)
	if raw == "" {
		return true, true, nil
	}
	enabled, err = strconv.ParseBool(raw)
	return enabled, true, err
}

func (s *FirewallService) ManagedPingEnabled() (bool, error) {
	enabled, _, err := firewallPingPreference()
	return enabled, err
}

func (s *FirewallService) reconcileManagedPingStateLocked(backendEnabled bool) error {
	preferred, configured, err := firewallPingPreference()
	if err != nil || !configured {
		return err
	}
	if !backendEnabled {
		return writeFirewallPingEnabled(true)
	}
	return writeFirewallPingEnabled(preferred)
}

func (s *FirewallService) ReconcileManagedPingState(ctx context.Context, safetyPort int) (FirewallManagedStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()

	backend, err := detectManagedFirewallBackend(ctx)
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	on, err := backend.enabled(ctx)
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	if err := s.reconcileManagedPingStateLocked(on); err != nil {
		return FirewallManagedStatus{}, err
	}
	return s.managedStatusSafeLocked(ctx, safetyPort)
}

func (s *FirewallService) SetManagedPingEnabledSafe(ctx context.Context, enabled bool, safetyPort int) (FirewallManagedStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()

	backend, err := detectManagedFirewallBackend(ctx)
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	on, err := backend.enabled(ctx)
	if err != nil {
		return FirewallManagedStatus{}, err
	}

	previousEffective, readErr := readFirewallPingEnabled()
	if readErr != nil {
		return FirewallManagedStatus{}, readErr
	}
	if on {
		if err := writeFirewallPingEnabled(enabled); err != nil {
			return FirewallManagedStatus{}, err
		}
	}
	if err := (&SettingService{}).setBool(firewallPingEnabledKey, enabled); err != nil {
		if on {
			_ = writeFirewallPingEnabled(previousEffective)
		}
		return FirewallManagedStatus{}, err
	}
	return s.managedStatusSafeLocked(ctx, safetyPort)
}
