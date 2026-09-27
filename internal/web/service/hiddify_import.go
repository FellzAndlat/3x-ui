package service

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

type hiddifyDomain struct {
	Domain         string   `json:"domain"`
	DownloadDomain string   `json:"download_domain"`
	ShowDomains    []string `json:"show_domains"`
}

type hiddifyConfig struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// HiddifyBackup is the legacy JSON export produced by Hiddify Panel. Only the
// users section is required for migration. Other sections are optional because
// Hiddify backups/restores may contain selected groups only.
type HiddifyBackup struct {
	Users []struct {
		UUID           string  `json:"uuid"`
		Name           string  `json:"name"`
		Comment        string  `json:"comment"`
		Enable         bool    `json:"enable"`
		IsActive       bool    `json:"is_active"`
		UsageLimitGB   float64 `json:"usage_limit_GB"`
		CurrentUsageGB float64 `json:"current_usage_GB"`
		PackageDays    int     `json:"package_days"`
		StartDate      *string `json:"start_date"`
		TelegramID     int64   `json:"telegram_id"`
		WGPrivateKey   string  `json:"wg_pk"`
		WGPublicKey    string  `json:"wg_pub"`
		WGPreSharedKey string  `json:"wg_psk"`
	} `json:"users"`
	Proxies []struct {
		Enable    bool   `json:"enable"`
		Proto     string `json:"proto"`
		Transport string `json:"transport"`
		L3        string `json:"l3"`
		CDN       string `json:"cdn"`
	} `json:"proxies"`
	Domains  []hiddifyDomain `json:"domains"`
	HConfigs []hiddifyConfig `json:"hconfigs"`
}

type HiddifyPreview struct {
	Users    int      `json:"users"`
	Warnings []string `json:"warnings"`
}

// hiddifyConfigString reads a value from this backup's hconfigs section.
// There is deliberately no default value: migration-specific values such as
// proxy_path_client must always come from the imported Hiddify backup itself.
func (b *HiddifyBackup) hiddifyConfigString(key string) (string, bool, error) {
	for _, config := range b.HConfigs {
		if strings.TrimSpace(config.Key) != key {
			continue
		}
		value, ok := config.Value.(string)
		if !ok {
			return "", true, fmt.Errorf("Hiddify %s must be a string", key)
		}
		return value, true, nil
	}
	return "", false, nil
}

func ParseHiddifyBackup(reader io.Reader) (*HiddifyBackup, HiddifyPreview, error) {
	var b HiddifyBackup
	var preview HiddifyPreview
	dec := json.NewDecoder(io.LimitReader(reader, 8<<20))
	if err := dec.Decode(&b); err != nil {
		return nil, preview, fmt.Errorf("invalid Hiddify JSON: %w", err)
	}
	if len(b.Users) == 0 {
		return nil, preview, fmt.Errorf("the Hiddify backup does not contain users")
	}
	seen := make(map[string]bool, len(b.Users))
	for _, user := range b.Users {
		if _, err := uuid.Parse(user.UUID); err != nil || seen[user.UUID] {
			return nil, preview, fmt.Errorf("backup contains an invalid or repeated user UUID")
		}
		seen[user.UUID] = true
		if math.IsNaN(user.UsageLimitGB) || math.IsNaN(user.CurrentUsageGB) || user.UsageLimitGB < 0 || user.CurrentUsageGB < 0 {
			return nil, preview, fmt.Errorf("backup contains an invalid traffic allowance")
		}
	}

	legacyAlias, err := b.HiddifyLegacySubscriptionAlias()
	if err != nil {
		return nil, preview, err
	}

	preview.Users = len(b.Users)
	preview.Warnings = []string{
		"Пользователи будут без подключений. После создания входящих подключений прикрепите к ним пользователей — тогда в подписках появятся профили.",
		"Для пользователей без даты первого подключения срок действия начнётся в день импорта.",
		"Ключи SSH (Ed25519) из Hiddify не переносятся: в модели пользователей панели нет полей для них.",
	}
	if legacyAlias.Path != "" {
		preview.Warnings = append([]string{
			fmt.Sprintf("Старые URL Hiddify с путём /%s/<UUID>/ будут сохранены и будут работать на любом домене, ведущем на сервер подписок.", legacyAlias.Path),
		}, preview.Warnings...)
	} else {
		preview.Warnings = append([]string{
			"В резервной копии не найден proxy_path_client: пользователи и их UUID будут восстановлены, но исходный путь старой ссылки Hiddify из этого файла определить нельзя.",
		}, preview.Warnings...)
	}

	return &b, preview, nil
}

// HiddifyLegacySubscriptionAlias returns the public path used by Hiddify for
// links like https://domain/<proxy_path_client>/<UUID>/. Domains are retained
// only as migration metadata; authorization of a legacy URL is path/UUID based.
func (b *HiddifyBackup) HiddifyLegacySubscriptionAlias() (HiddifyLegacySubscriptionAlias, error) {
	var alias HiddifyLegacySubscriptionAlias
	value, found, err := b.hiddifyConfigString("proxy_path_client")
	if err != nil {
		return alias, err
	}
	if !found {
		return alias, nil
	}
	path, err := normalizeHiddifyLegacySubPath(value)
	if err != nil {
		return alias, err
	}
	alias.Path = path
	if alias.Path == "" {
		return alias, nil
	}

	seenDomains := make(map[string]struct{})
	for _, item := range b.Domains {
		values := make([]string, 0, 2+len(item.ShowDomains))
		values = append(values, item.Domain, item.DownloadDomain)
		values = append(values, item.ShowDomains...)
		for _, value := range values {
			domain, err := normalizeHiddifyLegacyDomain(value)
			if err != nil || domain == "" {
				continue
			}
			if _, exists := seenDomains[domain]; exists {
				continue
			}
			seenDomains[domain] = struct{}{}
			alias.Domains = append(alias.Domains, domain)
		}
	}
	return alias, nil
}

// HiddifyClients preserves each user's UUID as the VPN credential and sub ID.
// Used traffic is subtracted from the allowance because Hiddify's counter is
// per user, while the new traffic records start at zero.
func (b *HiddifyBackup) HiddifyClients() ([]ClientCreatePayload, error) {
	legacyAlias, err := b.HiddifyLegacySubscriptionAlias()
	if err != nil {
		return nil, err
	}

	items := make([]ClientCreatePayload, 0, len(b.Users))
	for _, user := range b.Users {
		remaining := user.UsageLimitGB - user.CurrentUsageGB
		if remaining < 0 {
			remaining = 0
		}
		var total int64
		if user.UsageLimitGB > 0 {
			if remaining > float64(math.MaxInt64)/(1<<30) {
				return nil, fmt.Errorf("traffic allowance exceeds int64")
			}
			total = int64(remaining * (1 << 30))
			// Zero means unlimited in 3x-ui. A depleted limited account is disabled.
		}
		expiry := int64(0)
		if user.PackageDays > 0 {
			started := time.Now().UTC()
			if user.StartDate != nil && strings.TrimSpace(*user.StartDate) != "" {
				var err error
				started, err = time.Parse(time.RFC3339Nano, *user.StartDate)
				if err != nil {
					started, err = time.Parse("2006-01-02 15:04:05.999999", *user.StartDate)
				}
				if err != nil {
					started, err = time.Parse("2006-01-02", *user.StartDate)
				}
				if err != nil {
					return nil, fmt.Errorf("invalid Hiddify start date for user %s", user.UUID)
				}
			}
			expiry = started.AddDate(0, 0, user.PackageDays).UnixMilli()
		}
		items = append(items, ClientCreatePayload{
			Client: model.Client{
				ID: user.UUID, Password: user.UUID, Auth: user.UUID,
				SubID: user.UUID, Email: hiddifyEmail(user.Name, user.UUID),
				PrivateKey: user.WGPrivateKey, PublicKey: user.WGPublicKey,
				PreSharedKey: user.WGPreSharedKey,
				Group:        "Hiddify", Comment: strings.TrimSpace(user.Name + " " + user.Comment),
				TotalGB: total, ExpiryTime: expiry,
				Enable: user.Enable && user.IsActive && (user.UsageLimitGB == 0 || remaining > 0),
				TgID:   user.TelegramID,
			},
		})
	}
	if legacyAlias.Path != "" {
		if err := (&SettingService{}).AddHiddifyLegacySubscriptionAlias(legacyAlias); err != nil {
			return nil, fmt.Errorf("save legacy Hiddify subscription URL: %w", err)
		}
	}
	return items, nil
}

func hiddifyEmail(name, id string) string {
	var safe strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if safe.Len() >= 40 {
			break
		}
		if r == '/' || r == '\\' || unicode.IsSpace(r) || r < 0x20 || r == 0x7f {
			safe.WriteByte('_')
		} else {
			safe.WriteRune(r)
		}
	}
	if safe.Len() == 0 {
		safe.WriteString("user")
	}
	return "hiddify_" + safe.String() + "_" + id[:8]
}
