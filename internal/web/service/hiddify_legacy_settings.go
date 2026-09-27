package service

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
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

// looksLikeRecoverableHiddifyLegacySubPath keeps the subURI recovery path
// deliberately narrow. Hiddify proxy_path_client values are normally long,
// random-looking secrets; ordinary custom paths such as /sub/ or /clients/
// must never become host-independent legacy aliases by accident.
func looksLikeRecoverableHiddifyLegacySubPath(value string) bool {
	if len(value) < 16 {
		return false
	}
	hasLetter := false
	hasDigit := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}

// hiddifyLegacySubscriptionAliasFromSubURI recovers the imported secret path
// from the public subscription URL. Older/upgraded installations can retain a
// correct Hiddify-style subURI while the separate alias setting is absent or
// stale; in that state the UI generates a valid-looking URL but Telemt Nginx
// has no matching location and serves its decoy page instead.
func (s *SettingService) hiddifyLegacySubscriptionAliasFromSubURI() (HiddifyLegacySubscriptionAlias, bool) {
	current, err := s.GetSubURI()
	if err != nil || strings.TrimSpace(current) == "" {
		return HiddifyLegacySubscriptionAlias{}, false
	}
	parsed, err := url.Parse(strings.TrimSpace(current))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return HiddifyLegacySubscriptionAlias{}, false
	}

	path := strings.Trim(parsed.Path, "/")
	if path == "" || strings.Contains(path, "/") || !looksLikeRecoverableHiddifyLegacySubPath(path) {
		return HiddifyLegacySubscriptionAlias{}, false
	}
	path, err = normalizeHiddifyLegacySubPath(path)
	if err != nil || path == "" {
		return HiddifyLegacySubscriptionAlias{}, false
	}

	// Never reinterpret the panel's configured regular subscription path as a
	// Hiddify compatibility alias, even if an operator chose a long path.
	if subPath, subErr := s.GetSubPath(); subErr == nil && path == strings.Trim(strings.TrimSpace(subPath), "/") {
		return HiddifyLegacySubscriptionAlias{}, false
	}
	// /subs/ was used by older regular subscription configurations and is not a
	// Hiddify proxy_path_client secret.
	if strings.EqualFold(path, "subs") {
		return HiddifyLegacySubscriptionAlias{}, false
	}

	domain, err := normalizeHiddifyLegacyDomain(parsed.Hostname())
	if err != nil || domain == "" {
		return HiddifyLegacySubscriptionAlias{}, false
	}
	return HiddifyLegacySubscriptionAlias{Path: path, Domains: []string{domain}}, true
}

func (s *SettingService) GetHiddifyLegacySubscriptionAliases() ([]HiddifyLegacySubscriptionAlias, error) {
	raw, err := s.getString(hiddifyLegacySubscriptionAliasesSetting)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		raw = "[]"
	}

	var stored []HiddifyLegacySubscriptionAlias
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("decode legacy Hiddify subscription aliases: %w", err)
	}

	out := make([]HiddifyLegacySubscriptionAlias, 0, len(stored)+1)
	seenPaths := make(map[string]int, len(stored)+1)
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

	// Self-heal the route used by the URL that the panel is actually publishing.
	// Do not persist it here: Get* methods remain read-only, while the normal
	// Hiddify import path still stores aliases explicitly via Add/Save below.
	if recovered, ok := s.hiddifyLegacySubscriptionAliasFromSubURI(); ok {
		if index, exists := seenPaths[recovered.Path]; exists {
			out[index].Domains = mergeHiddifyDomains(out[index].Domains, recovered.Domains)
		} else {
			out = append(out, recovered)
		}
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

// HiddifySubscriptionURI uses the path from the backup and the public origin
// chosen by the operator. An empty origin reuses the current subscription
// origin, then falls back to the first domain present in the backup.
func (s *SettingService) HiddifySubscriptionURI(alias HiddifyLegacySubscriptionAlias, publicOrigin string) (string, error) {
	if alias.Path == "" {
		return "", nil
	}
	if strings.TrimSpace(publicOrigin) == "" {
		current, err := s.GetSubURI()
		if err != nil {
			return "", err
		}
		if parsed, err := url.Parse(current); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			publicOrigin = parsed.Scheme + "://" + parsed.Host
		} else if len(alias.Domains) > 0 {
			publicOrigin = "https://" + alias.Domains[0]
		} else {
			return "", nil
		}
	}
	publicOrigin = strings.TrimSpace(publicOrigin)
	if !strings.Contains(publicOrigin, "://") {
		publicOrigin = "https://" + publicOrigin
	}
	parsed, err := url.Parse(publicOrigin)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("enter a public subscription domain or an http(s) origin without a path")
	}
	if _, err := normalizeHiddifyLegacyDomain(parsed.Hostname()); err != nil {
		return "", fmt.Errorf("invalid subscription domain: %w", err)
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("invalid subscription port")
		}
	}
	// A public HTTPS URL cannot use the panel's plain HTTP subscription
	// listener. This commonly happens when the old /subs/ URL is copied as
	// the migration origin instead of the panel's HTTPS origin.
	if parsed.Scheme == "https" && parsed.Port() != "" {
		subPort, err := s.GetSubPort()
		if err != nil {
			return "", err
		}
		cert, err := s.GetSubCertFile()
		if err != nil {
			return "", err
		}
		key, err := s.GetSubKeyFile()
		if err != nil {
			return "", err
		}
		if parsed.Port() == strconv.Itoa(subPort) && (cert == "" || key == "") {
			return "", fmt.Errorf("HTTPS on subscription port %d requires a subscription TLS certificate; use the public panel HTTPS domain without this port", subPort)
		}
	}
	return parsed.Scheme + "://" + parsed.Host + "/" + alias.Path + "/", nil
}

// RepairHiddifySubscriptionURL updates addresses saved before the backup path
// was used for displayed subscription URLs. An explicit custom path and a
// multi-backup installation are left alone because their intent is ambiguous.
func (s *SettingService) RepairHiddifySubscriptionURL() (string, error) {
	aliases, err := s.GetHiddifyLegacySubscriptionAliases()
	if err != nil || len(aliases) != 1 {
		return "", err
	}
	current, err := s.GetSubURI()
	if err != nil || current == "" {
		return "", err
	}
	parsed, err := url.Parse(current)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", nil
	}
	subPath, err := s.GetSubPath()
	if err != nil {
		return "", err
	}
	path := strings.Trim(parsed.Path, "/")
	if path != strings.Trim(subPath, "/") && path != "subs" {
		return "", nil
	}
	// The old URL often points HTTPS at the separate HTTP listener. The
	// migrated path is also served by the panel on its public HTTPS origin.
	if parsed.Scheme == "https" && parsed.Port() != "" {
		subPort, err := s.GetSubPort()
		if err != nil {
			return "", err
		}
		cert, err := s.GetSubCertFile()
		if err != nil {
			return "", err
		}
		key, err := s.GetSubKeyFile()
		if err != nil {
			return "", err
		}
		if parsed.Port() == strconv.Itoa(subPort) && (cert == "" || key == "") {
			parsed.Host = parsed.Hostname()
			if strings.Contains(parsed.Host, ":") {
				parsed.Host = "[" + parsed.Host + "]"
			}
		}
	}
	updated := parsed.Scheme + "://" + parsed.Host + "/" + aliases[0].Path + "/"
	if updated == current {
		return "", nil
	}
	if err := s.setString("subURI", updated); err != nil {
		return "", err
	}
	return updated, nil
}

func (s *SettingService) SaveHiddifySubscriptionURL(alias HiddifyLegacySubscriptionAlias, uri string) error {
	if err := s.AddHiddifyLegacySubscriptionAlias(alias); err != nil {
		return err
	}
	if uri == "" {
		return nil
	}
	return s.setString("subURI", uri)
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
