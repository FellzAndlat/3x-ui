package service

import (
	"fmt"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
)

type AdBlockProfile struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Description         string `json:"description"`
	Sources             string `json:"sources"`
	UpdateIntervalHours int    `json:"updateIntervalHours"`
}

const (
	adBlockSourceAdAway      = "https://raw.githubusercontent.com/AdAway/adaway.github.io/master/hosts.txt"
	adBlockSourceStevenBlack = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
	adBlockSourcePeteLowe    = "https://pgl.yoyo.org/adservers/serverlist.php?hostformat=hosts&showintro=0&mimetype=plaintext"
)

func AdBlockProfiles() []AdBlockProfile {
	return []AdBlockProfile{
		{
			"balanced",
			"StevenBlack Unified · рекомендуемый",
			"Готовый объединённый hosts-список рекламы, трекеров и вредоносных доменов. Подходит большинству пользователей; обновление раз в сутки.",
			adBlockSourceStevenBlack,
			24,
		},
		{
			"adaway",
			"AdAway Official",
			"Официальный список AdAway для рекламы и мобильных трекеров; обновление раз в сутки.",
			adBlockSourceAdAway,
			24,
		},
		{
			"peterlowe",
			"Pete Lowe",
			"Компактный и давно поддерживаемый список рекламных серверов; обновление раз в сутки.",
			adBlockSourcePeteLowe,
			24,
		},
		{
			"mobile",
			"Реклама в приложениях",
			"AdGuard DNS: рекламные сети и трекеры, в том числе мобильные; обновление каждые 12 часов.",
			"https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt",
			12,
		},
		{
			"light",
			"Лёгкий",
			"OISD small: преимущественно реклама; меньше правил и ниже риск несовместимости, обновление раз в сутки.",
			"https://small.oisd.nl",
			24,
		},
		{
			"strict",
			"Расширенная защита",
			"OISD big: реклама, трекеры и вредоносные домены; обновление каждые 12 часов.",
			"https://big.oisd.nl",
			12,
		},
	}
}

func findAdBlockProfile(id string) (AdBlockProfile, bool) {
	for _, profile := range AdBlockProfiles() {
		if profile.ID == id {
			return profile, true
		}
	}
	return AdBlockProfile{}, false
}

func init() {
	defaultValueMap["adBlockProfile"] = "balanced"
	defaultValueMap["adBlockYoutubeMode"] = "off"
	defaultValueMap["adBlockSourceDomains"] = ""
	defaultValueMap["adBlockDownloadedSources"] = ""
	defaultValueMap["adBlockSourceCacheReady"] = "false"
}

func (s *SettingService) GetAdBlockProfile() (string, error) { return s.getString("adBlockProfile") }
func (s *SettingService) GetAdBlockYoutubeMode() (string, error) {
	return s.getString("adBlockYoutubeMode")
}

func validateYoutubeMode(mode string) error {
	switch mode {
	case "off", "compatible", "privacy":
		return nil
	}
	return fmt.Errorf("unsupported YouTube filtering mode %q", mode)
}

// Shared video/API hosts must remain reachable: advertising video cannot be
// distinguished from ordinary video at the domain-routing layer.
var youtubePlaybackDomains = []string{
	"youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "ggpht.com",
	"youtubei.googleapis.com", "youtube.googleapis.com",
}

const youtubeAncillaryAds = "googleads.g.doubleclick.net\nstatic.doubleclick.net\npagead2.googlesyndication.com\nad.doubleclick.net\n"

func effectiveAdBlockDomains(raw, allowlist, mode string) ([]string, error) {
	if err := validateYoutubeMode(mode); err != nil {
		return nil, err
	}
	allowed := splitAdBlockSettingValues(allowlist)
	if mode != "off" {
		allowed = append(allowed, youtubePlaybackDomains...)
	}
	if mode == "privacy" {
		raw += "\n" + youtubeAncillaryAds
	}
	return adblock.ParseDomains(strings.NewReader(raw), allowed)
}
