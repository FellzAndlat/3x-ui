package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
	"golang.org/x/net/idna"
)

type AdBlockDomainCheck struct {
	Policy          string              `json:"policy"`
	Profile         string              `json:"profile"`
	Domain          string              `json:"domain"`
	Blocked         bool                `json:"blocked"`
	Reason          string              `json:"reason"`
	Rule            string              `json:"rule"`
	Sources         []string            `json:"sources"`
	ContextRequired bool                `json:"contextRequired"`
	Application     *AdBlockApplication `json:"application"`
}

func domainMatch(rule, host string) bool {
	if strings.HasPrefix(rule, "domain:") {
		name := strings.TrimPrefix(rule, "domain:")
		return host == name || strings.HasSuffix(host, "."+name)
	}
	return host == rule
}
func (s *SettingService) CheckAdBlockDomain(input, inbound, client string) (*AdBlockDomainCheck, error) {
	host, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.TrimSpace(input), "."))
	if err != nil {
		return nil, err
	}
	host = strings.ToLower(host)
	rules, err := adblock.ParseDomains(strings.NewReader(host), nil)
	if err != nil || len(rules) != 1 || rules[0] != host || strings.ContainsAny(host, "/:^*# \t\n\r") {
		return nil, fmt.Errorf("enter a valid hostname without URL or port")
	}
	v, err := adBlockSnapshot()
	if err != nil {
		return nil, err
	}
	out := &AdBlockDomainCheck{Domain: host, Sources: []string{}, Reason: "Домен отсутствует в действующих списках"}
	out.Application, err = s.GetAdBlockApplication()
	if err != nil {
		return nil, err
	}
	selectedRaw := v["adBlockDomains"]
	out.Policy = "Общий профиль"
	out.Profile = v["adBlockProfile"]
	policies := []AdBlockPolicy{}
	if err := json.Unmarshal([]byte(v["adBlockPolicies"]), &policies); err != nil {
		return nil, err
	}
	var selected *AdBlockPolicy
	for i := range policies {
		p := &policies[i]
		if !p.Enabled {
			continue
		}
		_, never := scopePredicate(p.Scope)
		if never {
			continue
		}
		if (p.Scope.InboundMode != "all" && inbound == "") || (p.Scope.ClientMode != "all" && client == "") {
			out.ContextRequired = true
			break
		}
		if scopeMatches(p.Scope, inbound, client) {
			selected = p
			break
		}
	}
	if selected != nil {
		out.Policy = selected.Name
		out.Profile = selected.Profile
		if selected.Profile == "off" {
			selectedRaw = ""
		} else {
			data := map[string]string{}
			if err := json.Unmarshal([]byte(v["adBlockPolicyDomains"]), &data); err != nil {
				return nil, err
			}
			selectedRaw = data[selected.ID]
		}
	}
	effective, err := effectiveAdBlockDomains(selectedRaw, v["adBlockAllowlist"], v["adBlockYoutubeMode"])
	if out.Profile == "off" {
		effective = nil
	}

	if err != nil {
		return nil, err
	}
	for _, rule := range effective {
		if domainMatch(rule, host) {
			out.Rule = rule
			out.Blocked = true
			break
		}
	}
	var cache map[string]adblock.SourceCache
	if err := json.Unmarshal([]byte(v["adBlockSourceCache"]), &cache); err != nil {
		return nil, err
	}
	selectedSources := map[string]bool{}
	for _, url := range splitAdBlockSources(v["adBlockSources"]) {
		selectedSources[url] = true
	}
	if p := selected; p != nil {
		selectedSources = map[string]bool{}
		if profile, ok := findAdBlockProfile(p.Profile); ok {
			for _, url := range splitAdBlockSources(profile.Sources) {
				selectedSources[url] = true
			}
		}
	}
	for _, source := range adblock.CacheStatuses(cache) {
		if !selectedSources[source.URL] {
			continue
		}
		for _, rule := range cache[source.URL].Domains {
			if domainMatch(rule, host) {
				out.Sources = append(out.Sources, source.URL)
				break
			}
		}
	}
	custom, err := adblock.ParseDomains(strings.NewReader(v["adBlockCustomDomains"]), nil)
	if err != nil {
		return nil, err
	}
	for _, rule := range custom {
		if domainMatch(rule, host) {
			out.Sources = append(out.Sources, "Дополнительная блокировка")
			break
		}
	}
	if v["adBlockYoutubeMode"] == "privacy" {
		for _, name := range strings.Fields(youtubeAncillaryAds) {
			if host == name {
				out.Sources = append(out.Sources, "Сопутствующие рекламные домены Google")
			}
		}
	}
	if out.Blocked {
		out.Reason = "Совпадение с правилом блокировки"
	} else {
		raw, err := adblock.ParseDomains(strings.NewReader(selectedRaw), nil)
		if err != nil {
			return nil, err
		}
		for _, rule := range raw {
			if domainMatch(rule, host) {
				out.Rule = rule
				out.Reason = "Исключён разрешённым доменом или режимом совместимости YouTube; широкое правило может быть исключено целиком"
				break
			}
		}
	}
	if v["adBlockEnable"] != "true" {
		out.Blocked = false
		out.Reason = "AdBlock выключен"
		return out, nil
	}
	if adBlockPaused(v["adBlockPausedUntil"], time.Now()) {
		out.Blocked = false
		out.Reason = "Фильтрация временно приостановлена"
		return out, nil
	}
	scope, err := s.GetAdBlockScope()
	if err != nil {
		return nil, err
	}
	if out.ContextRequired || (scope.InboundMode != "all" && inbound == "") || (scope.ClientMode != "all" && client == "") {
		out.ContextRequired = true
		out.Blocked = false
		out.Reason = "Выберите inbound и клиента для проверки области фильтрации"
		return out, nil
	}
	if !scopeContains(scope.InboundMode, scope.Inbounds, inbound) || !scopeContains(scope.ClientMode, scope.Clients, client) {
		out.Blocked = false
		out.Reason = "Это подключение исключено из AdBlock"
	}
	if out.Profile == "off" && !out.ContextRequired {
		out.Blocked = false
		out.Reason = "Для этого подключения выбран профиль без фильтрации"
	}
	return out, nil
}
func (s *SettingService) GetAdBlockSourceStatuses() ([]adblock.SourceStatus, error) {
	raw, err := s.getString("adBlockSourceCache")
	if err != nil {
		return nil, err
	}
	var cache map[string]adblock.SourceCache
	if err := json.Unmarshal([]byte(raw), &cache); err != nil {
		return nil, err
	}
	configured, err := s.GetAdBlockSources()
	if err != nil {
		return nil, err
	}
	policies, err := s.GetAdBlockPolicies()
	if err != nil {
		return nil, err
	}
	selected := map[string]adblock.SourceCache{}
	for _, url := range append(splitAdBlockSources(configured), adBlockPolicySources(policies)...) {
		if cleaned, err := SanitizeHTTPURL(url); err == nil {
			url = cleaned
		}
		if entry, ok := cache[url]; ok {
			selected[url] = entry
		} else {
			selected[url] = adblock.SourceCache{LastError: "Источник ещё не загружен"}
		}
	}
	return adblock.CacheStatuses(selected), nil
}
