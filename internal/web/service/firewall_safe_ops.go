package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

func normalizeFirewallLabelUnicode(label string) string {
	label = strings.TrimSpace(label)
	if len(label) <= 120 {
		return label
	}
	var out strings.Builder
	out.Grow(120)
	for _, r := range label {
		width := utf8.RuneLen(r)
		if width < 0 || out.Len()+width > 120 {
			break
		}
		out.WriteRune(r)
	}
	return out.String()
}

func appendFrozenInboundRules(desired, managed []FirewallRule) []FirewallRule {
	seen := make(map[string]bool, len(desired)+len(managed))
	out := make([]FirewallRule, 0, len(desired)+len(managed))
	for _, rule := range desired {
		key := firewallRuleSpec(rule)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rule)
	}
	for _, rule := range managed {
		key := firewallRuleSpec(rule)
		if key == "" || seen[key] || rule.Source != "inbound" {
			continue
		}
		seen[key] = true
		out = append(out, rule)
	}
	sortFirewallRules(out)
	return out
}

func (s *FirewallService) syncManagedSafeLocked(ctx context.Context, backend firewallBackend, safetyPort int) error {
	if backend.name != "nftables" && backend.name != "iptables" {
		return s.syncManagedLocked(ctx, backend, safetyPort)
	}

	auto, err := firewallAutoSync()
	if err != nil {
		return err
	}
	desired, err := s.managedDesiredRules(auto, safetyPort)
	if err != nil {
		return err
	}
	if !auto {
		managed, err := loadManagedFirewallRules()
		if err != nil {
			return err
		}
		desired = appendFrozenInboundRules(desired, managed)
	}

	switch backend.name {
	case "nftables":
		if err := applyManagedNftables(ctx, backend.binary, desired); err != nil {
			return err
		}
	case "iptables":
		if err := applyManagedIPTablesSafe(ctx, backend.binary, desired); err != nil {
			// IPv4 may already have been installed when ip6tables fails. Remove
			// both owned chains so a failed operation never leaves a half-enabled
			// firewall behind.
			_ = removeManagedIPTables(ctx, backend.binary)
			return err
		}
	}
	return saveManagedOwnedRules(desired)
}

func decorateFrozenNativeStatus(status FirewallManagedStatus, managed []FirewallRule) FirewallManagedStatus {
	if status.AutoSync || (status.Backend != "nftables" && status.Backend != "iptables") {
		return status
	}
	seen := make(map[string]bool, len(status.Rules)+len(managed))
	for _, rule := range status.Rules {
		if key := firewallRuleSpec(rule); key != "" {
			seen[key] = true
		}
	}
	for _, rule := range managed {
		key := firewallRuleSpec(rule)
		if key == "" || seen[key] || rule.Source != "inbound" {
			continue
		}
		rule.Exists = status.Enabled
		rule.Owned = true
		status.Rules = append(status.Rules, rule)
		seen[key] = true
	}
	sortFirewallRules(status.Rules)
	return status
}

func (s *FirewallService) managedStatusSafeLocked(ctx context.Context, safetyPort int) (FirewallManagedStatus, error) {
	status, err := s.managedStatusLocked(ctx, safetyPort)
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	managed, err := loadManagedFirewallRules()
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	return decorateFrozenNativeStatus(status, managed), nil
}

func (s *FirewallService) GetManagedStatusSafe(ctx context.Context, safetyPort int) (FirewallManagedStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	return s.managedStatusSafeLocked(ctx, safetyPort)
}

func (s *FirewallService) SetManagedEnabledSafe(ctx context.Context, enabled bool, safetyPort int) (FirewallManagedStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	backend, err := detectManagedFirewallBackend(ctx)
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	if enabled {
		if err := s.syncManagedSafeLocked(ctx, backend, safetyPort); err != nil {
			return FirewallManagedStatus{}, err
		}
		if backend.name == "ufw" || backend.name == "firewalld" {
			if err := setFirewallBackendEnabled(ctx, backend, true); err != nil {
				return FirewallManagedStatus{}, err
			}
			if err := s.syncManagedSafeLocked(ctx, backend, safetyPort); err != nil {
				return FirewallManagedStatus{}, err
			}
		}
		if err := setFirewallManagedEnabledPreference(true); err != nil {
			return FirewallManagedStatus{}, err
		}
	} else {
		// Persist the disabled intent before removing runtime-only native rules so
		// a reboot cannot unexpectedly recreate them if the removal later fails.
		if err := setFirewallManagedEnabledPreference(false); err != nil {
			return FirewallManagedStatus{}, err
		}
		if err := disableManagedBackend(ctx, backend); err != nil {
			return FirewallManagedStatus{}, err
		}
	}
	return s.managedStatusSafeLocked(ctx, safetyPort)
}

func (s *FirewallService) SetManagedAutoSyncPreferenceSafe(ctx context.Context, enabled bool, safetyPort int) (FirewallManagedStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	if err := (&SettingService{}).setBool(firewallAutoSyncKey, enabled); err != nil {
		return FirewallManagedStatus{}, err
	}
	if enabled {
		backend, err := detectManagedFirewallBackend(ctx)
		if err != nil {
			return FirewallManagedStatus{}, err
		}
		on, err := backend.enabled(ctx)
		if err != nil {
			return FirewallManagedStatus{}, err
		}
		if on {
			if err := s.syncManagedSafeLocked(ctx, backend, safetyPort); err != nil {
				return FirewallManagedStatus{}, err
			}
		}
	}
	return s.managedStatusSafeLocked(ctx, safetyPort)
}

func (s *FirewallService) SyncManagedSafe(ctx context.Context, safetyPort int) (FirewallManagedStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	backend, err := detectManagedFirewallBackend(ctx)
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	if err := s.syncManagedSafeLocked(ctx, backend, safetyPort); err != nil {
		return FirewallManagedStatus{}, err
	}
	return s.managedStatusSafeLocked(ctx, safetyPort)
}

func (s *FirewallService) AddManagedManualRuleSafe(ctx context.Context, port int, protocol, label string, safetyPort int) (FirewallManagedStatus, error) {
	return s.changeManagedManualRuleSafe(ctx, port, protocol, label, safetyPort, true)
}

func (s *FirewallService) DeleteManagedManualRuleSafe(ctx context.Context, port int, protocol string, safetyPort int) (FirewallManagedStatus, error) {
	return s.changeManagedManualRuleSafe(ctx, port, protocol, "", safetyPort, false)
}

func (s *FirewallService) changeManagedManualRuleSafe(ctx context.Context, port int, protocol, label string, safetyPort int, add bool) (FirewallManagedStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	if port < 1 || port > 65535 {
		return FirewallManagedStatus{}, fmt.Errorf("invalid port %d", port)
	}
	protocols, err := normalizeFirewallProtocols(protocol)
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	manual, err := loadManualFirewallRules()
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	labels, err := loadFirewallManualLabels()
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	set := make(map[string]FirewallManualRule, len(manual)+2)
	for _, rule := range manual {
		set[firewallRuleKey(rule.Port, rule.Protocol)] = rule
	}
	label = normalizeFirewallLabelUnicode(label)
	for _, proto := range protocols {
		key := firewallRuleKey(port, proto)
		if add {
			set[key] = FirewallManualRule{Port: port, Protocol: proto}
			if label != "" {
				labels[key] = label
			} else {
				delete(labels, key)
			}
		} else {
			delete(set, key)
			delete(labels, key)
		}
	}
	manual = manual[:0]
	for _, rule := range set {
		manual = append(manual, rule)
	}
	sort.Slice(manual, func(i, j int) bool {
		return manual[i].Port < manual[j].Port || (manual[i].Port == manual[j].Port && manual[i].Protocol < manual[j].Protocol)
	})
	if err := saveFirewallJSON(firewallManualRulesKey, manual); err != nil {
		return FirewallManagedStatus{}, err
	}
	if err := saveFirewallManualLabels(labels); err != nil {
		return FirewallManagedStatus{}, err
	}
	backend, err := detectManagedFirewallBackend(ctx)
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	on, err := backend.enabled(ctx)
	if err != nil {
		return FirewallManagedStatus{}, err
	}
	if on {
		if err := s.syncManagedSafeLocked(ctx, backend, safetyPort); err != nil {
			return FirewallManagedStatus{}, err
		}
	}
	return s.managedStatusSafeLocked(ctx, safetyPort)
}
