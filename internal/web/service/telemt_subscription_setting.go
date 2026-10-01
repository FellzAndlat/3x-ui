package service

const telemtSubscriptionProxyEnableSettingKey = "telemtSubscriptionProxyEnable"

func init() {
	// Keep personal Telemt proxies disabled for subscriptions until the
	// administrator explicitly enables the feature.
	defaultValueMap[telemtSubscriptionProxyEnableSettingKey] = "false"
}

// GetTelemtSubscriptionProxyEnable reports whether personal Telemt proxies
// should be created and exposed for subscriptions.
func (s *SettingService) GetTelemtSubscriptionProxyEnable() (bool, error) {
	return s.getBool(telemtSubscriptionProxyEnableSettingKey)
}

// SetTelemtSubscriptionProxyEnable persists the administrator's preference.
// Disabling the setting intentionally does not remove already-created Telemt
// users; it only affects new creation/exposure paths.
func (s *SettingService) SetTelemtSubscriptionProxyEnable(enabled bool) error {
	return s.setBool(telemtSubscriptionProxyEnableSettingKey, enabled)
}
