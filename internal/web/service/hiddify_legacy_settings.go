package service

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
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

// normalizeHiddifyLegacySubscriptionURI keeps migrated Hiddify links on the
// public HTTPS vhost. Old imports may have persisted the dedicated subscription
// listener port (for example :2096); that listener is an internal implementation
// detail for migrated URLs, which must continue to use standard HTTPS/443.
func normalizeHiddifyLegacySubscriptionURI(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return raw
	}
	host := parsed.Hostname()
	if host == "" {
		return raw
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	parsed.Scheme = "https"
	parsed.Host = host
	return parsed.String()
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
		} else if panelDomain, err := s.GetWebDomain(); err != nil {
			return "", err
		} else if panelDomain != "" {
			publicOrigin = "https://" + panelDomain
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
	// Migrated links always use the HTTPS vhost on the standard port. The
	// subscription listener's own port remains the default for other users.
	host := parsed.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "https://" + host + "/" + alias.Path + "/", nil
}

// MigrateHiddifySubscriptionURLs undoes older imports that stored the Hiddify
// path in the global subURI. Only records matching the old importer identity
// receive the legacy URL; everyone else returns to the normal listener URL.
func (s *SettingService) MigrateHiddifySubscriptionURLs() (int, error) {
	aliases, err := s.GetHiddifyLegacySubscriptionAliases()
	if err != nil || len(aliases) == 0 {
		return 0, err
	}
	current, err := s.GetSubURI()
	if err != nil || current == "" {
		return 0, err
	}
	parsed, err := url.Parse(current)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return 0, nil
	}
	var matched bool
	for _, alias := range aliases {
		if strings.Trim(parsed.Path, "/") == alias.Path {
			matched = true
			break
		}
	}
	if !matched {
		return 0, nil
	}
	// The alias may have been recovered only from the old global URI. Keep it
	// persisted before the global URI is cleared.
	if err := s.saveHiddifyLegacySubscriptionAliases(aliases); err != nil {
		return 0, err
	}
	host := parsed.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	legacyURI := "https://" + host + parsed.Path
	if !strings.HasSuffix(legacyURI, "/") {
		legacyURI += "/"
	}
	count := 0
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		var records []model.ClientRecord
		if err := tx.Where("email LIKE ?", "hiddify_%").Find(&records).Error; err != nil {
			return err
		}
		found := false
		for _, rec := range records {
			if !isImportedHiddifyClient(rec) {
				continue
			}
			found = true
			if rec.HiddifySubURI != "" {
				continue
			}
			if err := tx.Model(&rec).Update("hiddify_sub_uri", legacyURI).Error; err != nil {
				return err
			}
			count++
		}
		if !found {
			return nil
		}
		return tx.Model(&model.Setting{}).Where("key = ?", "subURI").Update("value", "").Error
	})
	return count, err
}

func isImportedHiddifyClient(rec model.ClientRecord) bool {
	if rec.SubID != rec.UUID {
		return false
	}
	id, err := uuid.Parse(rec.UUID)
	return err == nil && strings.HasPrefix(rec.Email, "hiddify_") && strings.HasSuffix(strings.ToLower(rec.Email), "_"+id.String()[:8])
}

func (s *SettingService) SaveHiddifySubscriptionURL(alias HiddifyLegacySubscriptionAlias, uri string, items []ClientCreatePayload) error {
	if err := s.AddHiddifyLegacySubscriptionAlias(alias); err != nil {
		return err
	}
	uri = normalizeHiddifyLegacySubscriptionURI(uri)
	if uri == "" {
		return nil
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		for _, item := range items {
			client := item.Client
			var rec model.ClientRecord
			if err := tx.Where("email = ? AND sub_id = ? AND uuid = ?", client.Email, client.SubID, client.ID).First(&rec).Error; err != nil {
				if database.IsNotFound(err) {
					continue // The import skipped this user.
				}
				return err
			}
			if !isImportedHiddifyClient(rec) {
				continue
			}
			if err := tx.Model(&rec).Update("hiddify_sub_uri", uri).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SettingService) GetHiddifySubscriptionURIs() (map[string]string, error) {
	var records []model.ClientRecord
	if err := database.GetDB().Model(&model.ClientRecord{}).Select("sub_id", "hiddify_sub_uri").Where("hiddify_sub_uri <> ?", "").Find(&records).Error; err != nil {
		return nil, err
	}
	urls := make(map[string]string, len(records))
	for _, rec := range records {
		if rec.SubID != "" {
			urls[rec.SubID] = normalizeHiddifyLegacySubscriptionURI(rec.HiddifySubURI)
		}
	}
	return urls, nil
}

func (s *SettingService) IsHiddifySubscriptionPath(subID, requestPath string) bool {
	var rec model.ClientRecord
	if err := database.GetDB().Select("hiddify_sub_uri").Where("sub_id = ? AND hiddify_sub_uri <> ?", subID, "").First(&rec).Error; err != nil {
		return false
	}
	uri, err := url.Parse(rec.HiddifySubURI)
	if err != nil {
		return false
	}
	return strings.HasPrefix(requestPath, strings.TrimSuffix(uri.Path, "/")+"/")
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
