package service

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock/youtubeproxy"
	"github.com/SawaMEN/3x-ui/v3/internal/config"
)

type AdBlockServerSettings struct {
	Enabled  bool                 `json:"enabled"`
	Outbound string               `json:"outbound"`
	Limits   youtubeproxy.Options `json:"limits"`
}

func init() {
	raw, _ := json.Marshal(AdBlockServerSettings{Limits: youtubeproxy.DefaultOptions()})
	defaultValueMap["adBlockServer"] = string(raw)
}
func AdBlockServerCADir() string { return filepath.Join(config.GetDBFolderPath(), "youtube-filter") }
func (s *SettingService) GetAdBlockServer() (AdBlockServerSettings, error) {
	raw, err := s.getString("adBlockServer")
	var settings AdBlockServerSettings
	if err == nil {
		err = json.Unmarshal([]byte(raw), &settings)
	}
	return settings, err
}
func (settings *AdBlockServerSettings) validate() error {
	settings.Outbound = strings.TrimSpace(settings.Outbound)
	if len(settings.Outbound) > 128 || strings.ContainsAny(settings.Outbound, "\r\n\x00") || strings.HasPrefix(settings.Outbound, "youtube-") {
		return fmt.Errorf("invalid server filter outbound")
	}
	return settings.Limits.Validate()
}
func (s *SettingService) AdBlockServerEffective() (bool, error) {
	server, err := s.GetAdBlockServer()
	if err != nil {
		return false, err
	}
	plan, err := s.readAdBlockPlan()
	return server.Enabled && plan.Enabled, err
}

func (s *SettingService) AdBlockSnapshotForRollback() (map[string]string, error) {
	return adBlockSnapshot()
}
func (s *SettingService) RestoreAdBlockSnapshot(values map[string]string) error {
	adBlockUpdateMu.Lock()
	defer adBlockUpdateMu.Unlock()
	filtered := map[string]string{}
	for k, v := range values {
		if strings.HasPrefix(k, "adBlock") {
			filtered[k] = v
		}
	}
	return saveAdBlockValues(filtered)
}

func commitManagedYouTube(core string) {
	s := &SettingService{}
	selected, err := s.GetCoreType()
	if err != nil || selected != core {
		return
	}
	enabled, err := s.AdBlockServerEffective()
	if err == nil {
		youtubeproxy.Commit(enabled)
	}
}

func (s *SettingService) AdBlockServerOutboundOptions() ([]string, error) {
	core, err := s.GetCoreType()
	if err != nil {
		return nil, err
	}
	var raw string
	if core == CoreTypeSingBox {
		raw, err = s.GetSingBoxConfigTemplate()
	} else {
		raw, err = s.GetXrayConfigTemplate()
	}
	if err != nil {
		return nil, err
	}
	var data struct {
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
		Endpoints []struct {
			Tag string `json:"tag"`
		} `json:"endpoints"`
		Routing struct {
			Balancers []struct {
				Tag string `json:"tag"`
			} `json:"balancers"`
		} `json:"routing"`
	}
	if err = json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, entry := range data.Outbounds {
		set[entry.Tag] = true
	}
	for _, entry := range data.Endpoints {
		set[entry.Tag] = true
	}
	for _, entry := range data.Routing.Balancers {
		set[entry.Tag] = true
	}
	tags := []string{}
	for tag := range set {
		if tag != "" && !strings.HasPrefix(tag, "youtube-") {
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	return tags, nil
}
