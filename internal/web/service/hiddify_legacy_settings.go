package service

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
)

const hiddifyLegacySubscriptionAliasesSetting = "hiddifyLegacySubscriptionAliases"

type HiddifyLegacySubscriptionAlias struct {
	Path    string   `json:"path"`
	Domains []string `json:"domains,omitempty"`
}

func init() {
	defaultValueMap[hiddifyLegacySubscriptionAliasesSetting] = "[]"
}

func normalizeHiddifyLegacySubPath(value string) (string, error) {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return "", nil
	}
	if len(value) > 128 {
		return "", fmt.Errorf("Hiddify proxy_path_client is too long")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("Hiddify proxy_path_client contains unsupported characters")
	}
	return value, nil
}

func normalizeHiddifyLegacyDomain(value string) (string, error) {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if value == "" {
		return "", nil
	}
	if len(value) > 253 || strings.ContainsAny(value, "/\\@") {
		return "", fmt.Errorf("invalid Hiddify domain")
	}
	if ip := net.ParseIP(strings.Trim(value, "[]")); ip != nil {
		return ip.String(), nil
	}

	labels := strings.Split(value, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid Hiddify domain")
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return "", fmt.Errorf("invalid Hiddify domain")
		}
	}
	return value, nil
}

func normalizeHiddifyLegacySubscriptionAlias(alias HiddifyLegacySubscriptionAlias) (HiddifyLegacySubscriptionAlias, error) {
	path, err := normalizeHiddifyLegacySubPath(alias.Path)
	if err != nil {
		return HiddifyLegacySubscriptionAlias{}, err
	}
	alias.Path = path
	if alias.Path == "" {
		alias.Domains = nil
		return alias, nil
	}

	seen := make(map[string]struct{}, len(alias.Domains))
	domains := make([]string, 0, len(alias.Domains))
	for _, value := range alias.Domains {
		domain, err := normalizeHiddifyLegacyDomain(value)
		if err != nil || domain == "" {
			continue
		}
		if _, exists := seen[domain]; exists {
			continue
		}
		seen[domain] = struct{}{}
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	alias.Domains = domains
	return alias, nil
}

func (s *SettingService) GetHiddifyLegacySubscriptionAliases() ([]HiddifyLegacySubscriptionAlias, error) {
	raw, err := s.getString(hiddifyLegacySubscriptionAliasesSetting)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	var stored []HiddifyLegacySubscriptionAlias
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("decode legacy Hiddify subscription aliases: %w", err)
	}

	out := make([]HiddifyLegacySubscriptionAlias, 0, len(stored))
	seenPaths := make(map[string]int, len(stored))
	for _, item := range stored {
		alias, err := normalizeHiddifyLegacySubscriptionAlias(item)
		if err != nil || alias.Path == "" {
			continue
		}
		if index, exists := seenPaths[alias.Path]; exists {
			out[index].Domains = mergeHiddifyDomains(out[index].Domains, alias.Domains)
			continue
		}
		seenPaths[alias.Path] = len(out)
		out = append(out, alias)
	}
	return out, nil
}

func (s *SettingService) AddHiddifyLegacySubscriptionAlias(value HiddifyLegacySubscriptionAlias) error {
	alias, err := normalizeHiddifyLegacySubscriptionAlias(value)
	if err != nil {
		return err
	}
	if alias.Path == "" {
		return nil
	}

	aliases, err := s.GetHiddifyLegacySubscriptionAliases()
	if err != nil {
		return err
	}
	for i := range aliases {
		if aliases[i].Path != alias.Path {
			continue
		}
		aliases[i].Domains = mergeHiddifyDomains(aliases[i].Domains, alias.Domains)
		return s.saveHiddifyLegacySubscriptionAliases(aliases)
	}

	aliases = append(aliases, alias)
	return s.saveHiddifyLegacySubscriptionAliases(aliases)
}

func (s *SettingService) saveHiddifyLegacySubscriptionAliases(aliases []HiddifyLegacySubscriptionAlias) error {
	data, err := json.Marshal(aliases)
	if err != nil {
		return err
	}
	return s.setString(hiddifyLegacySubscriptionAliasesSetting, string(data))
}

func mergeHiddifyDomains(left, right []string) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	out := make([]string, 0, len(left)+len(right))
	for _, list := range [][]string{left, right} {
		for _, value := range list {
			domain, err := normalizeHiddifyLegacyDomain(value)
			if err != nil || domain == "" {
				continue
			}
			if _, exists := seen[domain]; exists {
				continue
			}
			seen[domain] = struct{}{}
			out = append(out, domain)
		}
	}
	sort.Strings(out)
	return out
}
