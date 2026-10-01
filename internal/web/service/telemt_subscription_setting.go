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
// The Telemt controller reconciles all existing subscription users before the
// new value is committed, so saved subscription pages follow this setting too.
func (s *SettingService) SetTelemtSubscriptionProxyEnable(enabled bool) error {
	return s.setBool(telemtSubscriptionProxyEnableSettingKey, enabled)
}
