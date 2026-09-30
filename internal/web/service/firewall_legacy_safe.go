package service

import "context"

// removeManagedLegacyRules removes only rules that were created by 3x-ui.
// It deliberately leaves UFW/firewalld themselves running so disabling the
// panel feature cannot disable unrelated administrator firewall policy.
func removeManagedLegacyRules(ctx context.Context, backend firewallBackend) error {
	existing, err := listFirewallRules(ctx, backend)
	if err != nil {
		return err
	}
	managed, err := loadManagedFirewallRules()
	if err != nil {
		return err
	}
	for _, rule := range managed {
		key := firewallRuleSpec(rule)
		if key == "" || !rule.Owned || !existing[key] {
			continue
		}
		if err := deleteFirewallRule(ctx, backend, rule); err != nil {
			return err
		}
	}
	return saveFirewallJSON(firewallManagedRulesKey, []FirewallRule{})
}

func disableManagedBackendSafe(ctx context.Context, backend firewallBackend) error {
	if backend.name == "ufw" || backend.name == "firewalld" {
		return removeManagedLegacyRules(ctx, backend)
	}
	return disableManagedBackend(ctx, backend)
}
