package service

const telemtSubscriptionProxyEnableSettingKey = "telemtSubscriptionProxyEnable"

func init() {
	// Preserve the existing behavior: personal Telemt proxies are available for
	// subscriptions unless the administrator explicitly disables the feature.
	defaultValueMap[telemtSubscriptionProxyEnableSettingKey] = "true"
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
