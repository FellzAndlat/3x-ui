package service

import (
	"strconv"
	"strings"
)

const firewallManagedEnabledKey = "firewallManagedEnabled"

func firewallManagedEnabledPreference() (bool, error) {
	raw, err := firewallSetting(firewallManagedEnabledKey, "false")
	if err != nil {
		return false, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}

func setFirewallManagedEnabledPreference(enabled bool) error {
	return (&SettingService{}).setBool(firewallManagedEnabledKey, enabled)
}

func shouldRestoreManagedNativeFirewall(backend string, active, desired bool) bool {
	if active || !desired {
		return false
	}
	return backend == "nftables" || backend == "iptables"
}
