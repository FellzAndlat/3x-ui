package service

import "context"

// MigrateManagedBackendIfNeeded removes a 3x-ui-owned native nftables/iptables
// layer when an administrator subsequently enables UFW or firewalld. Running
// both filtering layers at once could make the stricter stale layer override
// the newly enabled system firewall.
func (s *FirewallService) MigrateManagedBackendIfNeeded(ctx context.Context) error {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	return migrateManagedBackendIfNeededLocked(ctx)
}

func migrateManagedBackendIfNeededLocked(ctx context.Context) error {
	native, ok := detectOwnedNativeFirewallBackend(ctx)
	if !ok {
		return nil
	}
	external, err := detectFirewallBackend(ctx)
	if err != nil {
		return nil
	}
	on, err := external.enabled(ctx)
	if err != nil || !on {
		return err
	}
	return disableManagedBackend(ctx, native)
}
