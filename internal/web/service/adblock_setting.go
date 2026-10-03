package service

import (
	"fmt"
	"strconv"
)

const (
	DefaultAdBlockUpdateIntervalHours = 24
	MinAdBlockUpdateIntervalHours     = 1
	MaxAdBlockUpdateIntervalHours     = 168
)

func init() {
	defaultValueMap["adBlockEnable"] = "false"
	defaultValueMap["adBlockDomains"] = ""
	defaultValueMap["adBlockAllowlist"] = ""
	defaultValueMap["adBlockAutoUpdate"] = "true"
	defaultValueMap["adBlockUpdateIntervalHours"] = strconv.Itoa(DefaultAdBlockUpdateIntervalHours)
}

func (s *SettingService) GetAdBlockEnable() (bool, error) {
	return s.getBool("adBlockEnable")
}

func (s *SettingService) SetAdBlockEnable(value bool) error {
	return s.setBool("adBlockEnable", value)
}

func (s *SettingService) GetAdBlockDomains() (string, error) {
	return s.getString("adBlockDomains")
}

func (s *SettingService) SetAdBlockDomains(value string) error {
	return s.setString("adBlockDomains", value)
}

func (s *SettingService) GetAdBlockAllowlist() (string, error) {
	return s.getString("adBlockAllowlist")
}

func (s *SettingService) SetAdBlockAllowlist(value string) error {
	return s.setString("adBlockAllowlist", value)
}

func (s *SettingService) GetAdBlockAutoUpdate() (bool, error) {
	return s.getBool("adBlockAutoUpdate")
}

func (s *SettingService) SetAdBlockAutoUpdate(value bool) error {
	return s.setBool("adBlockAutoUpdate", value)
}

func (s *SettingService) GetAdBlockUpdateIntervalHours() (int, error) {
	value, err := s.getInt("adBlockUpdateIntervalHours")
	if err != nil {
		return 0, err
	}
	if value < MinAdBlockUpdateIntervalHours || value > MaxAdBlockUpdateIntervalHours {
		return 0, fmt.Errorf(
			"adblock update interval must be between %d and %d hours",
			MinAdBlockUpdateIntervalHours,
			MaxAdBlockUpdateIntervalHours,
		)
	}
	return value, nil
}

func (s *SettingService) SetAdBlockUpdateIntervalHours(value int) error {
	if value < MinAdBlockUpdateIntervalHours || value > MaxAdBlockUpdateIntervalHours {
		return fmt.Errorf(
			"adblock update interval must be between %d and %d hours",
			MinAdBlockUpdateIntervalHours,
			MaxAdBlockUpdateIntervalHours,
		)
	}
	return s.setString("adBlockUpdateIntervalHours", strconv.Itoa(value))
}
