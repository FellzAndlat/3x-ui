package service

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

type HiddifyExportStats struct {
	Exported int
	Skipped  int
}

type hiddifyExportBackup struct {
	Users    []hiddifyExportUser `json:"users"`
	Domains  []hiddifyDomain     `json:"domains,omitempty"`
	HConfigs []hiddifyConfig     `json:"hconfigs,omitempty"`
}

type hiddifyExportUser struct {
	UUID           string  `json:"uuid"`
	Name           string  `json:"name"`
	Comment        string  `json:"comment,omitempty"`
	Enable         bool    `json:"enable"`
	IsActive       bool    `json:"is_active"`
	UsageLimitGB   float64 `json:"usage_limit_GB"`
	CurrentUsageGB float64 `json:"current_usage_GB"`
	PackageDays    int     `json:"package_days,omitempty"`
	StartDate      *string `json:"start_date,omitempty"`
	TelegramID     int64   `json:"telegram_id,omitempty"`
	WGPrivateKey   string  `json:"wg_pk,omitempty"`
	WGPublicKey    string  `json:"wg_pub,omitempty"`
	WGPreSharedKey string  `json:"wg_psk,omitempty"`
}

// ExportHiddifyBackup creates a minimal Hiddify-compatible backup containing
// panel clients plus the currently preferred legacy proxy_path_client alias.
// It deliberately does not export unrelated panel settings or credentials.
func (s *ClientService) ExportHiddifyBackup(settingSvc *SettingService) ([]byte, HiddifyExportStats, error) {
	var stats HiddifyExportStats
	if settingSvc == nil {
		return nil, stats, fmt.Errorf("settings service is required")
	}

	var records []model.ClientRecord
	if err := database.GetDB().Order("id ASC").Find(&records).Error; err != nil {
		return nil, stats, fmt.Errorf("load clients for Hiddify export: %w", err)
	}
	if len(records) == 0 {
		return nil, stats, fmt.Errorf("there are no clients to export")
	}

	aliases, err := settingSvc.GetHiddifyLegacySubscriptionAliases()
	if err != nil {
		return nil, stats, err
	}
	currentURI, err := settingSvc.GetSubURI()
	if err != nil {
		return nil, stats, err
	}
	alias := selectHiddifyExportAlias(aliases, currentURI)

	backup, stats := buildHiddifyExportBackup(records, alias, currentURI)
	if len(backup.Users) == 0 {
		return nil, stats, fmt.Errorf("no clients have a UUID-compatible identity for Hiddify export")
	}
	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return nil, stats, fmt.Errorf("encode Hiddify export: %w", err)
	}
	data = append(data, '\n')
	return data, stats, nil
}

func buildHiddifyExportBackup(records []model.ClientRecord, alias HiddifyLegacySubscriptionAlias, currentURI string) (hiddifyExportBackup, HiddifyExportStats) {
	backup := hiddifyExportBackup{Users: make([]hiddifyExportUser, 0, len(records))}
	stats := HiddifyExportStats{}
	seenUUID := make(map[string]struct{}, len(records))

	for _, record := range records {
		id, ok := hiddifyExportUUID(record)
		if !ok {
			stats.Skipped++
			continue
		}
		if _, exists := seenUUID[id]; exists {
			stats.Skipped++
			continue
		}
		seenUUID[id] = struct{}{}

		packageDays, startDate := hiddifyExportExpiry(record.ExpiryTime)
		backup.Users = append(backup.Users, hiddifyExportUser{
			UUID:           id,
			Name:           record.Email,
			Comment:        record.Comment,
			Enable:         record.Enable,
			IsActive:       record.Enable,
			UsageLimitGB:   float64(record.TotalGB) / float64(int64(1)<<30),
			CurrentUsageGB: 0,
			PackageDays:    packageDays,
			StartDate:      startDate,
			TelegramID:     record.TgID,
			WGPrivateKey:   record.PrivateKey,
			WGPublicKey:    record.PublicKey,
			WGPreSharedKey: record.PreSharedKey,
		})
		stats.Exported++
	}

	if alias.Path != "" {
		backup.HConfigs = []hiddifyConfig{{Key: "proxy_path_client", Value: alias.Path}}
		for _, domain := range hiddifyExportDomains(alias, currentURI) {
			backup.Domains = append(backup.Domains, hiddifyDomain{
				Domain:         domain,
				DownloadDomain: domain,
				ShowDomains:    []string{domain},
			})
		}
	}
	return backup, stats
}

func hiddifyExportUUID(record model.ClientRecord) (string, bool) {
	for _, value := range []string{record.UUID, record.SubID, record.Password, record.Auth} {
		parsed, err := uuid.Parse(strings.TrimSpace(value))
		if err == nil {
			return parsed.String(), true
		}
	}
	return "", false
}

// Hiddify expresses expiry as start_date + package_days. Using exactly one day
// and moving start_date back by one day preserves the panel's millisecond expiry
// on a round trip without rounding the remaining lifetime to whole days.
func hiddifyExportExpiry(expiryTime int64) (int, *string) {
	if expiryTime <= 0 {
		return 0, nil
	}
	start := time.UnixMilli(expiryTime).UTC().AddDate(0, 0, -1).Format("2006-01-02 15:04:05.000000")
	return 1, &start
}

func selectHiddifyExportAlias(aliases []HiddifyLegacySubscriptionAlias, currentURI string) HiddifyLegacySubscriptionAlias {
	if len(aliases) == 0 {
		return HiddifyLegacySubscriptionAlias{}
	}
	if parsed, err := url.Parse(strings.TrimSpace(currentURI)); err == nil {
		path := strings.Trim(parsed.Path, "/")
		for _, alias := range aliases {
			if alias.Path == path {
				return alias
			}
		}
	}
	// Aliases are persisted in import order; the latest one is the least
	// surprising fallback when subURI no longer points at a legacy path.
	return aliases[len(aliases)-1]
}

func hiddifyExportDomains(alias HiddifyLegacySubscriptionAlias, currentURI string) []string {
	domains := append([]string(nil), alias.Domains...)
	if parsed, err := url.Parse(strings.TrimSpace(currentURI)); err == nil && strings.Trim(parsed.Path, "/") == alias.Path {
		if domain, err := normalizeHiddifyLegacyDomain(parsed.Hostname()); err == nil && domain != "" {
			domains = append(domains, domain)
		}
	}
	return mergeHiddifyDomains(nil, domains)
}
