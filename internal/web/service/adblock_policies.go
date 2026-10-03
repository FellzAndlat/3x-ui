package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
	"github.com/SawaMEN/3x-ui/v3/internal/util/netsafe"
)

const maxAdBlockPolicies = 8

type AdBlockPolicy struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Enabled bool         `json:"enabled"`
	Profile string       `json:"profile"`
	Scope   AdBlockScope `json:"scope"`
}

func init() {
	defaultValueMap["adBlockPolicies"] = "[]"
	defaultValueMap["adBlockPolicyDomains"] = "{}"
}
func (s *SettingService) GetAdBlockPolicies() ([]AdBlockPolicy, error) {
	raw, err := s.getString("adBlockPolicies")
	if err != nil {
		return nil, err
	}
	policies := []AdBlockPolicy{}
	err = json.Unmarshal([]byte(raw), &policies)
	return policies, err
}
func validateAdBlockPolicies(policies []AdBlockPolicy) error {
	if len(policies) > maxAdBlockPolicies {
		return fmt.Errorf("maximum %d AdBlock policies", maxAdBlockPolicies)
	}
	ids := map[string]bool{}
	pattern := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	for i := range policies {
		p := &policies[i]
		p.Name = strings.TrimSpace(p.Name)
		if !pattern.MatchString(p.ID) || ids[p.ID] {
			return fmt.Errorf("invalid or duplicate policy ID")
		}
		ids[p.ID] = true
		if len(p.Name) == 0 || len(p.Name) > 128 {
			return fmt.Errorf("policy name must contain 1–128 bytes")
		}
		if _, ok := findAdBlockProfile(p.Profile); !ok && p.Profile != "off" {
			return fmt.Errorf("unsupported policy profile %q", p.Profile)
		}
		if err := normalizeAdBlockScope(&p.Scope); err != nil {
			return err
		}
	}
	return nil
}
func adBlockPolicySources(policies []AdBlockPolicy) []string {
	urls := []string{}
	for _, p := range policies {
		if p.Enabled && p.Profile != "off" {
			profile, _ := findAdBlockProfile(p.Profile)
			urls = append(urls, splitAdBlockSources(profile.Sources)...)
		}
	}
	return urls
}
func newAdBlockManager() (adblock.Manager, func()) {
	transport := &http.Transport{DialContext: netsafe.SSRFGuardedDialContext}
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
		transport.DialContext = netsafe.SSRFGuardedDialContext
	}
	transport.Proxy = nil
	return adblock.Manager{Client: &http.Client{Timeout: 45 * time.Second, Transport: transport}, ValidateURL: func(raw string) (string, error) { return SanitizePublicHTTPURL(raw, false) }}, transport.CloseIdleConnections
}

// Policy caches share downloads with the default profile, but each policy
// retains its own domain set. A weaker/off policy must not inherit stricter rules.
func (s *SettingService) buildAdBlockPolicyData(ctx context.Context, policies []AdBlockPolicy, baseSources, custom, allowlist, mode string, useCache bool, result *AdBlockUpdateResult) (string, error) {
	raw, err := s.getString("adBlockSourceCache")
	if err != nil {
		return "", err
	}
	cache := map[string]adblock.SourceCache{}
	if err := json.Unmarshal([]byte(raw), &cache); err != nil {
		return "", err
	}
	baseCache := map[string]adblock.SourceCache{}
	if result.SourceCache != "" {
		if err := json.Unmarshal([]byte(result.SourceCache), &baseCache); err != nil {
			return "", err
		}
		for url, entry := range baseCache {
			cache[url] = entry
		}
	}
	manager, close := newAdBlockManager()
	defer close()
	seen := map[string]bool{}
	downloaded := false
	for _, url := range adBlockPolicySources(policies) {
		if cleaned, err := SanitizeHTTPURL(url); err == nil {
			url = cleaned
		}
		if seen[url] {
			continue
		}
		seen[url] = true
		if _, ready := baseCache[url]; ready {
			continue
		}
		hadCache := len(cache[url].Domains) > 0
		if useCache && hadCache {
			if cache[url].LastError != "" {
				result.Warnings = append(result.Warnings, url+": "+cache[url].LastError)
			}
			continue
		}
		fetched, err := manager.FetchCached(ctx, []string{url}, cache, useCache)
		if err != nil {
			return "", err
		}
		for key, entry := range fetched.Cache {
			cache[key] = entry
		}
		result.BytesRead += fetched.BytesRead
		result.Warnings = append(result.Warnings, fetched.Warnings...)
		downloaded = downloaded || !useCache || !hadCache
	}
	selected := map[string]adblock.SourceCache{}
	sourceURLs := map[string]bool{}
	for _, url := range append(splitAdBlockSources(baseSources), adBlockPolicySources(policies)...) {
		if cleaned, err := SanitizeHTTPURL(url); err == nil {
			url = cleaned
		}
		sourceURLs[url] = true
		if entry, ok := cache[url]; ok {
			selected[url] = entry
		}
	}
	if len(sourceURLs) > adblock.DefaultMaxSources {
		return "", fmt.Errorf("too many sources across all profiles")
	}
	entries := 0
	for _, entry := range selected {
		entries += len(entry.Domains)
	}
	if entries > 4*adblock.DefaultMaxDomains {
		return "", fmt.Errorf("policy source caches exceed 1,200,000 entries")
	}
	cacheJSON, err := json.Marshal(selected)
	if err != nil {
		return "", err
	}
	if len(cacheJSON) > 64<<20 {
		return "", fmt.Errorf("policy source caches exceed 64 MiB")
	}
	result.SourceCache = string(cacheJSON)
	if downloaded {
		result.Cached = false
		result.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	result.SourceCount = len(sourceURLs)
	data := map[string]string{}
	effectiveUnion := map[string]bool{}
	baseDomains, err := effectiveAdBlockDomains(result.SourceDomains+"\n"+custom, allowlist, mode)
	if err != nil {
		return "", err
	}
	for _, domain := range baseDomains {
		effectiveUnion[domain] = true
	}
	for _, p := range policies {
		if !p.Enabled || p.Profile == "off" {
			continue
		}
		profile, _ := findAdBlockProfile(p.Profile)
		set := map[string]bool{}
		for _, url := range splitAdBlockSources(profile.Sources) {
			if cleaned, err := SanitizeHTTPURL(url); err == nil {
				url = cleaned
			}
			for _, domain := range cache[url].Domains {
				set[domain] = true
			}
		}
		domains := make([]string, 0, len(set))
		for domain := range set {
			domains = append(domains, domain)
		}
		sort.Strings(domains)
		combined, err := adblock.ParseDomains(strings.NewReader(strings.Join(domains, "\n")+"\n"+custom), nil)
		if err != nil {
			return "", err
		}
		data[p.ID] = strings.Join(combined, "\n")
		effective, err := effectiveAdBlockDomains(data[p.ID], allowlist, mode)
		if err != nil {
			return "", err
		}
		for _, domain := range effective {
			effectiveUnion[domain] = true
		}
	}
	result.DomainCount = len(effectiveUnion)
	out, err := json.Marshal(data)
	return string(out), err
}
func scopeMatches(scope AdBlockScope, inbound, client string) bool {
	return scopeContains(scope.InboundMode, scope.Inbounds, inbound) && scopeContains(scope.ClientMode, scope.Clients, client)
}
func selectedAdBlockPolicy(policies []AdBlockPolicy, inbound, client string) *AdBlockPolicy {
	for i := range policies {
		if policies[i].Enabled && scopeMatches(policies[i].Scope, inbound, client) {
			return &policies[i]
		}
	}
	return nil
}
