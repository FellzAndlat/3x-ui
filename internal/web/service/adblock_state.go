package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock/youtubeproxy"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

type AdBlockScope struct {
	InboundMode string   `json:"inboundMode"`
	Inbounds    []string `json:"inbounds"`
	ClientMode  string   `json:"clientMode"`
	Clients     []string `json:"clients"`
}
type AdBlockApplication struct {
	Pending   bool   `json:"pending"`
	LastError string `json:"lastError"`
	AppliedAt string `json:"appliedAt"`
	NextRetry string `json:"nextRetry"`
}

func init() {
	defaultValueMap["adBlockScope"] = `{"inboundMode":"all","inbounds":[],"clientMode":"all","clients":[]}`
	defaultValueMap["adBlockPausedUntil"] = ""
	defaultValueMap["adBlockApplyPending"] = "false"
	defaultValueMap["adBlockApplyError"] = ""
	defaultValueMap["adBlockAppliedAt"] = ""
	defaultValueMap["adBlockAppliedCore"] = ""
	defaultValueMap["adBlockApplyNextRetry"] = ""
	defaultValueMap["adBlockSourceCache"] = "{}"
}
func adBlockSnapshot() (map[string]string, error) {
	var rows []model.Setting
	if err := database.GetDB().Where("key LIKE ? OR key = ?", "adBlock%", "coreType").Order("id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	values := make(map[string]string)
	for key, value := range defaultValueMap {
		if strings.HasPrefix(key, "adBlock") || key == "coreType" {
			values[key] = value
		}
	}
	for _, row := range rows {
		values[row.Key] = row.Value
	}
	return values, nil
}
func (s *SettingService) GetAdBlockScope() (AdBlockScope, error) {
	raw, err := s.getString("adBlockScope")
	if err != nil {
		return AdBlockScope{}, err
	}
	var scope AdBlockScope
	err = json.Unmarshal([]byte(raw), &scope)
	return scope, err
}
func normalizeAdBlockScope(scope *AdBlockScope) error {
	for _, mode := range []*string{&scope.InboundMode, &scope.ClientMode} {
		if *mode == "" {
			*mode = "all"
		}
		if *mode != "all" && *mode != "include" && *mode != "exclude" {
			return fmt.Errorf("invalid AdBlock scope mode %q", *mode)
		}
	}
	for _, items := range []*[]string{&scope.Inbounds, &scope.Clients} {
		set := map[string]bool{}
		for _, item := range *items {
			item = strings.TrimSpace(item)
			if item == "" || len(item) > 256 || strings.ContainsAny(item, "\r\n\x00") {
				return fmt.Errorf("invalid AdBlock scope identifier")
			}
			set[item] = true
		}
		if len(set) > 2000 {
			return fmt.Errorf("too many AdBlock scope identifiers")
		}
		*items = make([]string, 0, len(set))
		for item := range set {
			*items = append(*items, item)
		}
		sort.Strings(*items)
	}
	if scope.InboundMode == "all" {
		scope.Inbounds = []string{}
	}
	if scope.ClientMode == "all" {
		scope.Clients = []string{}
	}
	return nil
}
func adBlockPaused(raw string, now time.Time) bool {
	until, err := time.Parse(time.RFC3339, raw)
	return err == nil && now.Before(until)
}
func adBlockFingerprint(values map[string]string, now time.Time) (string, error) {
	plan, err := adBlockPlanAt(values, now)
	if err != nil {
		return "", err
	}
	payload := []any{values["coreType"], plan.Base}
	if plan.Enabled {
		payload = append(payload, plan.Scope)
		var server AdBlockServerSettings
		if err := json.Unmarshal([]byte(values["adBlockServer"]), &server); err != nil {
			return "", err
		}
		if server.Enabled {
			payload = append(payload, server)
		}
		for _, p := range plan.Policies {
			if p.Enabled {
				payload = append(payload, []any{p.ID, p.Scope, plan.Domains[p.ID]})
			}
		}
	}
	data, _ := json.Marshal(payload)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// Every change affecting runtime rules persists its apply intent in the same transaction.
// Interval/profile metadata alone never causes a restart.
func saveAdBlockConfigValues(values map[string]string) error {
	current, err := adBlockSnapshot()
	if err != nil {
		return err
	}
	before, err := adBlockFingerprint(current, time.Now())
	if err != nil {
		return err
	}
	for key, value := range values {
		current[key] = value
	}
	after, err := adBlockFingerprint(current, time.Now())
	if err != nil {
		return err
	}
	if before != after {
		values["adBlockApplyPending"] = "true"
		values["adBlockApplyError"] = ""
		values["adBlockApplyNextRetry"] = ""
	}
	return saveAdBlockValues(values)
}
func (s *SettingService) GetAdBlockApplication() (*AdBlockApplication, error) {
	pending, err := s.getBool("adBlockApplyPending")
	if err != nil {
		return nil, err
	}
	message, err := s.getString("adBlockApplyError")
	if err != nil {
		return nil, err
	}
	at, err := s.getString("adBlockAppliedAt")
	if err != nil {
		return nil, err
	}
	retry, err := s.getString("adBlockApplyNextRetry")
	if err != nil {
		return nil, err
	}
	return &AdBlockApplication{Pending: pending, LastError: message, AppliedAt: at, NextRetry: retry}, nil
}
func (s *SettingService) ReconcileAdBlockApplication() error {
	if youtubeproxy.NeedsRecovery() {
		if err := saveAdBlockValues(map[string]string{"adBlockApplyPending": "true"}); err != nil {
			return err
		}
	}
	// Read only small scheduling metadata on each tick, not the domain/cache blobs.
	paused, err := s.GetAdBlockPausedUntil()
	if err != nil {
		return err
	}
	if paused != "" && !adBlockPaused(paused, time.Now()) {
		enabled, err := s.GetAdBlockEnable()
		if err != nil {
			return err
		}
		values := map[string]string{"adBlockPausedUntil": ""}
		if enabled {
			values["adBlockApplyPending"] = "true"
			values["adBlockApplyNextRetry"] = ""
		}
		return saveAdBlockValues(values)
	}
	appliedCore, err := s.getString("adBlockAppliedCore")
	if err != nil {
		return err
	}
	core, err := s.GetCoreType()
	if err != nil {
		return err
	}
	if appliedCore != "" && appliedCore != core {
		return saveAdBlockValues(map[string]string{"adBlockApplyPending": "true"})
	}
	return nil
}
func (s *SettingService) MarkAdBlockApplied(applyErr error) error {
	if applyErr != nil {
		return saveAdBlockValues(map[string]string{"adBlockApplyPending": "true", "adBlockApplyError": applyErr.Error(), "adBlockApplyNextRetry": time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)})
	}
	core, err := s.GetCoreType()
	if err != nil {
		return err
	}
	enabled, err := s.AdBlockServerEffective()
	if err != nil {
		return err
	}
	youtubeproxy.Commit(enabled)
	return saveAdBlockValues(map[string]string{"adBlockApplyPending": "false", "adBlockApplyError": "", "adBlockAppliedAt": time.Now().UTC().Format(time.RFC3339), "adBlockAppliedCore": core, "adBlockApplyNextRetry": ""})
}
func (s *SettingService) SetAdBlockPause(minutes int) error {
	if minutes != 0 && minutes != 5 && minutes != 15 && minutes != 60 {
		return fmt.Errorf("pause must be 0, 5, 15 or 60 minutes")
	}
	adBlockUpdateMu.Lock()
	defer adBlockUpdateMu.Unlock()
	if err := s.ReconcileAdBlockApplication(); err != nil {
		return err
	}
	until := ""
	if minutes > 0 {
		until = time.Now().UTC().Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339)
	}
	return saveAdBlockConfigValues(map[string]string{"adBlockPausedUntil": until})
}
func scopeContains(mode string, items []string, value string) bool {
	found := false
	for _, item := range items {
		if item == value {
			found = true
			break
		}
	}
	switch mode {
	case "include":
		return found
	case "exclude":
		return !found
	default:
		return true
	}
}

func (s *SettingService) GetAdBlockPausedUntil() (string, error) {
	return s.getString("adBlockPausedUntil")
}
