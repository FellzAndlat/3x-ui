package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func init() {
	defaultValueMap["adBlockLastAttempt"] = ""
	defaultValueMap["adBlockLastError"] = ""
	defaultValueMap["adBlockRetryCount"] = "0"
	defaultValueMap["adBlockNextRetry"] = ""
}

type AdBlockAutomationStatus struct {
	LastAttempt string `json:"lastAttempt"`
	LastError   string `json:"lastError"`
	RetryCount  int    `json:"retryCount"`
	NextRetry   string `json:"nextRetry"`
}

func (s *SettingService) GetAdBlockAutomationStatus() (*AdBlockAutomationStatus, error) {
	attempt, err := s.getString("adBlockLastAttempt")
	if err != nil {
		return nil, err
	}
	lastError, err := s.getString("adBlockLastError")
	if err != nil {
		return nil, err
	}
	count, err := s.getInt("adBlockRetryCount")
	if err != nil {
		return nil, err
	}
	retry, err := s.getString("adBlockNextRetry")
	if err != nil {
		return nil, err
	}
	return &AdBlockAutomationStatus{attempt, lastError, count, retry}, nil
}

func adBlockRetryDelay(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	if failures > 8 {
		failures = 8
	}
	delay := 5 * time.Minute * time.Duration(1<<uint(failures-1))
	if delay > 6*time.Hour {
		delay = 6 * time.Hour
	}
	return delay
}

func (s *SettingService) adBlockFailureValues(err error) (map[string]string, error) {
	count, readErr := s.getInt("adBlockRetryCount")
	if readErr != nil {
		return nil, readErr
	}
	if count < 0 {
		count = 0
	}
	if count < 8 {
		count++
	}
	now := time.Now().UTC()
	message := err.Error()
	if len(message) > 4096 {
		message = message[:4096]
	}
	return map[string]string{
		"adBlockLastAttempt": now.Format(time.RFC3339), "adBlockLastError": message,
		"adBlockRetryCount": strconv.Itoa(count), "adBlockNextRetry": now.Add(adBlockRetryDelay(count)).Format(time.RFC3339),
	}, nil
}

// Retry deadlines are bounded; a clock jump cannot suppress updates forever.
func (s *SettingService) AdBlockRetryDue(now time.Time) (bool, error) {
	raw, err := s.getString("adBlockNextRetry")
	if err != nil || raw == "" {
		return true, err
	}
	retry, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return true, nil
	}
	if retry.After(now.Add(6*time.Hour + 5*time.Minute)) {
		return true, nil
	}
	return !now.Before(retry), nil
}

func adBlockSuccessfulUpdateValues(result *AdBlockUpdateResult, domains, sources string) map[string]string {
	values := map[string]string{
		"adBlockDomains": domains, "adBlockDomainCount": strconv.Itoa(result.DomainCount),
	}
	if result.SourceCache != "" {
		values["adBlockSourceCache"] = result.SourceCache
	}
	if !result.Cached {
		values["adBlockLastUpdate"] = result.UpdatedAt
		values["adBlockSourceCount"] = strconv.Itoa(result.SourceCount)
		values["adBlockSourceDomains"] = result.SourceDomains
		values["adBlockDownloadedSources"] = normalizedAdBlockSources(sources)
		values["adBlockSourceCacheReady"] = "true"
		values["adBlockLastAttempt"] = result.UpdatedAt
		values["adBlockLastError"] = ""
		values["adBlockRetryCount"] = "0"
		values["adBlockNextRetry"] = ""
	}
	return values
}

func normalizedAdBlockSources(raw string) string { return strings.Join(splitAdBlockSources(raw), "\n") }

func (s *SettingService) recordAdBlockFailure(err error) error {
	values, readErr := s.adBlockFailureValues(err)
	if readErr != nil {
		return readErr
	}
	return saveAdBlockValues(values)
}
func (s *SettingService) adBlockUpdateValues(result *AdBlockUpdateResult, domains, sources string) (map[string]string, error) {
	values := adBlockSuccessfulUpdateValues(result, domains, sources)
	if len(result.Warnings) > 0 && !result.Cached {
		failure, err := s.adBlockFailureValues(fmt.Errorf("%s", strings.Join(result.Warnings, "; ")))
		if err != nil {
			return nil, err
		}
		for key, value := range failure {
			values[key] = value
		}
	}
	return values, nil
}
