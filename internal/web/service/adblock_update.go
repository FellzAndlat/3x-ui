package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"gorm.io/gorm"
)

const DefaultAdBlockSources = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"

var adBlockUpdateMu sync.Mutex

func init() {
	defaultValueMap["adBlockSources"] = DefaultAdBlockSources
	defaultValueMap["adBlockCustomDomains"] = ""
	defaultValueMap["adBlockLastUpdate"] = ""
	defaultValueMap["adBlockDomainCount"] = "0"
	defaultValueMap["adBlockSourceCount"] = "0"
}

type AdBlockUpdateResult struct {
	DomainCount   int      `json:"domainCount"`
	SourceCount   int      `json:"sourceCount"`
	BytesRead     int64    `json:"bytesRead"`
	UpdatedAt     string   `json:"updatedAt"`
	Changed       bool     `json:"changed"`
	Cached        bool     `json:"cached"`
	SourceDomains string   `json:"-"`
	SourceCache   string   `json:"-"`
	Warnings      []string `json:"warnings,omitempty"`
}

func (s *SettingService) GetAdBlockSources() (string, error) {
	return s.getString("adBlockSources")
}

func (s *SettingService) SetAdBlockSources(value string) error {
	for _, source := range splitAdBlockSources(value) {
		if _, err := SanitizeHTTPURL(source); err != nil {
			return fmt.Errorf("invalid adblock source %q: %w", source, err)
		}
	}
	return s.setString("adBlockSources", value)
}

func (s *SettingService) GetAdBlockCustomDomains() (string, error) {
	return s.getString("adBlockCustomDomains")
}

func (s *SettingService) SetAdBlockCustomDomains(value string) error {
	return s.setString("adBlockCustomDomains", value)
}

func (s *SettingService) GetAdBlockLastUpdate() (string, error) {
	return s.getString("adBlockLastUpdate")
}

func (s *SettingService) GetAdBlockDomainCount() (int, error) {
	return s.getInt("adBlockDomainCount")
}

func (s *SettingService) GetAdBlockSourceCount() (int, error) {
	return s.getInt("adBlockSourceCount")
}

// UpdateAdBlock downloads every configured source and only replaces the
// effective domain list after all downloads and parsing have succeeded. A
// failed update therefore leaves the last-known-good list active. Updates are
// serialized here so API and background refreshes cannot race each other.
func (s *SettingService) UpdateAdBlock(ctx context.Context) (result *AdBlockUpdateResult, updateErr error) {
	adBlockUpdateMu.Lock()
	defer adBlockUpdateMu.Unlock()
	defer func() {
		if updateErr != nil {
			_ = s.recordAdBlockFailure(updateErr)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	previousDomains, err := s.GetAdBlockDomains()
	if err != nil {
		return nil, err
	}
	sourcesRaw, err := s.GetAdBlockSources()
	if err != nil {
		return nil, err
	}
	customRaw, err := s.GetAdBlockCustomDomains()
	if err != nil {
		return nil, err
	}
	allowlistRaw, err := s.GetAdBlockAllowlist()
	if err != nil {
		return nil, err
	}

	mode, err := s.GetAdBlockYoutubeMode()
	if err != nil {
		return nil, err
	}
	result, joinedDomains, err := s.buildAdBlock(ctx, sourcesRaw, customRaw, allowlistRaw, mode, false)
	if err != nil {
		return nil, err
	}
	policies, err := s.GetAdBlockPolicies()
	if err != nil {
		return nil, err
	}
	previousPolicyDomains, err := s.getString("adBlockPolicyDomains")
	if err != nil {
		return nil, err
	}
	policyDomains, err := s.buildAdBlockPolicyData(ctx, policies, sourcesRaw, customRaw, allowlistRaw, mode, false, result)
	if err != nil {
		return nil, err
	}
	result.Changed = previousDomains != joinedDomains || previousPolicyDomains != policyDomains
	values, err := s.adBlockUpdateValues(result, joinedDomains, sourcesRaw)
	if err != nil {
		return nil, err
	}
	values["adBlockPolicyDomains"] = policyDomains
	if err := saveAdBlockConfigValues(values); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *SettingService) buildAdBlock(ctx context.Context, sourcesRaw, customRaw, allowlistRaw, mode string, useCache bool) (*AdBlockUpdateResult, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	manager, close := newAdBlockManager()
	defer close()

	fetched := adblock.FetchResult{}
	cached := false
	if useCache {
		ready, err := s.getBool("adBlockSourceCacheReady")
		if err != nil {
			return nil, "", err
		}
		cachedSources, err := s.getString("adBlockDownloadedSources")
		if err != nil {
			return nil, "", err
		}
		if ready && cachedSources == normalizedAdBlockSources(sourcesRaw) {
			raw, err := s.getString("adBlockSourceDomains")
			if err != nil {
				return nil, "", err
			}
			fetched.Domains, err = adblock.ParseDomains(strings.NewReader(raw), nil)
			if err != nil {
				return nil, "", err
			}
			fetched.SourceCount, err = s.GetAdBlockSourceCount()
			if err != nil {
				return nil, "", err
			}
			cached = true
		}
	}
	if !cached {
		var err error
		rawCache, err := s.getString("adBlockSourceCache")
		if err != nil {
			return nil, "", err
		}
		previous := map[string]adblock.SourceCache{}
		if err := json.Unmarshal([]byte(rawCache), &previous); err != nil {
			return nil, "", err
		}
		fetched, err = manager.FetchCached(ctx, splitAdBlockSources(sourcesRaw), previous, useCache)
		if err != nil {
			return nil, "", err
		}
	}

	custom, err := adblock.ParseDomains(strings.NewReader(customRaw), nil)
	if err != nil {
		return nil, "", fmt.Errorf("parse custom adblock domains: %w", err)
	}

	set := make(map[string]struct{}, len(fetched.Domains)+len(custom))
	for _, domain := range fetched.Domains {
		set[domain] = struct{}{}
	}
	for _, domain := range custom {
		set[domain] = struct{}{}
	}
	domains := make([]string, 0, len(set))
	for domain := range set {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	if len(domains) > adblock.DefaultMaxDomains {
		return nil, "", fmt.Errorf("combined adblock list exceeds %d domains", adblock.DefaultMaxDomains)
	}

	joinedDomains := strings.Join(domains, "\n")
	effective, err := effectiveAdBlockDomains(joinedDomains, allowlistRaw, mode)
	if err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	if cached {
		updatedAt, err = s.GetAdBlockLastUpdate()
		if err != nil {
			return nil, "", err
		}
	}

	cacheJSON := ""
	if fetched.Cache != nil {
		raw, err := json.Marshal(fetched.Cache)
		if err != nil {
			return nil, "", err
		}
		if len(raw) > 64<<20 {
			return nil, "", fmt.Errorf("source caches exceed 64 MiB")
		}
		cacheJSON = string(raw)
	}
	return &AdBlockUpdateResult{
		DomainCount:   len(effective),
		SourceCount:   fetched.SourceCount,
		BytesRead:     fetched.BytesRead,
		UpdatedAt:     updatedAt,
		Cached:        cached,
		SourceDomains: strings.Join(fetched.Domains, "\n"),
		SourceCache:   cacheJSON, Warnings: fetched.Warnings,
	}, joinedDomains, nil
}

func splitAdBlockSources(raw string) []string {
	lines := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' })
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		result = append(result, line)
	}
	return result
}

func splitAdBlockSettingValues(raw string) []string {
	var values []string
	for _, line := range strings.Split(raw, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		values = append(values, strings.FieldsFunc(line, func(r rune) bool {
			return r == '\r' || r == ',' || r == ';' || r == ' ' || r == '\t'
		})...)
	}
	return values
}

// SaveAdBlockSettings validates and downloads before persisting anything. A
// source failure cannot partially change the allowlist or disable protection.
type AdBlockSettings struct {
	Server                            *AdBlockServerSettings
	Policies                          *[]AdBlockPolicy
	Scope                             *AdBlockScope
	Enabled                           bool
	Sources, CustomDomains, Allowlist string
	AutoUpdate                        *bool
	UpdateIntervalHours               *int
	Profile                           *string
	YoutubeMode                       *string
}

func (s *SettingService) SaveAdBlockSettings(ctx context.Context, settings AdBlockSettings) error {
	adBlockUpdateMu.Lock()
	defer adBlockUpdateMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	policies, err := s.GetAdBlockPolicies()
	if err != nil {
		return err
	}
	if settings.Policies != nil {
		policies = *settings.Policies
		if policies == nil {
			policies = []AdBlockPolicy{}
		}
	}
	if err := validateAdBlockPolicies(policies); err != nil {
		return err
	}
	profile, err := s.GetAdBlockProfile()
	if err != nil {
		return err
	}
	mode, err := s.GetAdBlockYoutubeMode()
	if err != nil {
		return err
	}
	if settings.Profile != nil {
		profile = *settings.Profile
	} else {
		previousSources, err := s.GetAdBlockSources()
		if err != nil {
			return err
		}
		if normalizedAdBlockSources(previousSources) != normalizedAdBlockSources(settings.Sources) {
			profile = "custom"
		}
	}
	if profile != "custom" {
		preset, ok := findAdBlockProfile(profile)
		if !ok {
			return fmt.Errorf("unsupported adblock profile %q", profile)
		}
		settings.Sources = preset.Sources
		if settings.AutoUpdate == nil {
			auto := true
			settings.AutoUpdate = &auto
		}
		if settings.UpdateIntervalHours == nil {
			interval := preset.UpdateIntervalHours
			settings.UpdateIntervalHours = &interval
		}
	}
	if settings.YoutubeMode != nil {
		mode = *settings.YoutubeMode
	}
	if err := validateYoutubeMode(mode); err != nil {
		return err
	}

	sources := splitAdBlockSources(settings.Sources)
	if len(sources) > adblock.DefaultMaxSources {
		return fmt.Errorf("too many sources (maximum %d)", adblock.DefaultMaxSources)
	}
	for _, source := range sources {
		if _, err := SanitizeHTTPURL(source); err != nil {
			return fmt.Errorf("invalid adblock source: %w", err)
		}
	}
	if settings.UpdateIntervalHours != nil && (*settings.UpdateIntervalHours < MinAdBlockUpdateIntervalHours || *settings.UpdateIntervalHours > MaxAdBlockUpdateIntervalHours) {
		return fmt.Errorf("adblock update interval must be between %d and %d hours", MinAdBlockUpdateIntervalHours, MaxAdBlockUpdateIntervalHours)
	}
	values := map[string]string{
		"adBlockEnable":        strconv.FormatBool(settings.Enabled),
		"adBlockProfile":       profile,
		"adBlockYoutubeMode":   mode,
		"adBlockSources":       settings.Sources,
		"adBlockCustomDomains": settings.CustomDomains,
		"adBlockAllowlist":     settings.Allowlist,
	}
	if settings.Server != nil {
		if err := settings.Server.validate(); err != nil {
			return err
		}
		raw, _ := json.Marshal(settings.Server)
		values["adBlockServer"] = string(raw)
	}
	policyJSON, _ := json.Marshal(policies)
	values["adBlockPolicies"] = string(policyJSON)
	if settings.Scope != nil {
		if err := normalizeAdBlockScope(settings.Scope); err != nil {
			return err
		}
		raw, _ := json.Marshal(settings.Scope)
		values["adBlockScope"] = string(raw)
	}
	if settings.AutoUpdate != nil {
		values["adBlockAutoUpdate"] = strconv.FormatBool(*settings.AutoUpdate)
	}
	if settings.UpdateIntervalHours != nil {
		values["adBlockUpdateIntervalHours"] = strconv.Itoa(*settings.UpdateIntervalHours)
	}
	if settings.Enabled {
		result, domains, err := s.buildAdBlock(ctx, settings.Sources, settings.CustomDomains, settings.Allowlist, mode, true)
		if err != nil {
			return err
		}
		policyDomains, err := s.buildAdBlockPolicyData(ctx, policies, settings.Sources, settings.CustomDomains, settings.Allowlist, mode, true, result)
		if err != nil {
			return err
		}
		values["adBlockPolicyDomains"] = policyDomains
		updateValues, err := s.adBlockUpdateValues(result, domains, settings.Sources)
		if err != nil {
			return err
		}
		for key, value := range updateValues {
			values[key] = value
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return saveAdBlockConfigValues(values)
}

func saveAdBlockValues(values map[string]string) error {
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		// Settings keys are not unique in older panel databases. Use the existing
		// row by primary key rather than relying on a key uniqueness constraint.
		for key, value := range values {
			var setting model.Setting
			err := tx.Where("key = ?", key).First(&setting).Error
			if err != nil && !database.IsNotFound(err) {
				return err
			}
			if database.IsNotFound(err) {
				if err := tx.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Model(&setting).Update("value", value).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
