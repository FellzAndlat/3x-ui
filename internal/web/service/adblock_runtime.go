package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

// Configuration editors and database reads return the original template.
// Filtering is applied only after the complete runtime configuration is built.
func (s *SettingService) adBlockRuntimePolicy() ([]string, AdBlockScope, error) {
	// One query observes a consistent settings snapshot during atomic updates.
	var settings []model.Setting
	if err := database.GetDB().Where("key IN ?", []string{"adBlockEnable", "adBlockDomains", "adBlockAllowlist", "adBlockYoutubeMode", "adBlockPausedUntil", "adBlockScope"}).Order("id DESC").Find(&settings).Error; err != nil {
		return nil, AdBlockScope{}, err
	}
	values := map[string]string{"adBlockEnable": "false", "adBlockYoutubeMode": "off", "adBlockScope": defaultValueMap["adBlockScope"]}
	for _, setting := range settings {
		values[setting.Key] = setting.Value
	}
	enabled, err := strconv.ParseBool(values["adBlockEnable"])
	if err != nil || !enabled || adBlockPaused(values["adBlockPausedUntil"], time.Now()) {
		return nil, AdBlockScope{}, err
	}
	var scope AdBlockScope
	if err := json.Unmarshal([]byte(values["adBlockScope"]), &scope); err != nil {
		return nil, scope, err
	}
	domains, err := effectiveAdBlockDomains(values["adBlockDomains"], values["adBlockAllowlist"], values["adBlockYoutubeMode"])
	return domains, scope, err
}

func (s *SettingService) adBlockRuntimeDomains() ([]string, error) {
	domains, _, err := s.adBlockRuntimePolicy()
	return domains, err
}

func (s *SettingService) applyXrayAdBlock(cfg *xray.Config) error {
	policies, err := s.GetAdBlockPolicies()
	if err != nil {
		return err
	}
	if len(policies) > 0 {
		plan, err := s.readAdBlockPlan()
		if err != nil {
			return err
		}
		return applyXrayPolicyPlan(cfg, plan)
	}
	domains, scope, err := s.adBlockRuntimePolicy()
	if err != nil {
		return err
	}
	if len(domains) == 0 && !bytes.Contains(cfg.RouterConfig, []byte(adblock.OutboundTag)) && !bytes.Contains(cfg.OutboundConfigs, []byte(adblock.OutboundTag)) {
		return nil
	}
	routing, outbounds, err := adblock.ApplyXrayRouting(cfg.RouterConfig, cfg.OutboundConfigs, domains)
	if err != nil {
		return err
	}
	cfg.RouterConfig, cfg.OutboundConfigs = routing, outbounds
	if len(domains) > 0 {
		if err := applyXrayAdBlockScope(cfg, scope); err != nil {
			return err
		}
	}
	if len(domains) == 0 {
		return nil
	}
	for i := range cfg.InboundConfigs {
		inbound := &cfg.InboundConfigs[i]
		// Do not sniff the panel's management/API and infrastructure traffic.
		if inbound.Tag == "api" || inbound.Tag == PanelEgressInboundTag || !scopeContains(scope.InboundMode, scope.Inbounds, inbound.Tag) {
			continue
		}
		sniff, err := adblock.XraySniffing(inbound.Sniffing)
		if err != nil {
			return fmt.Errorf("adblock inbound %q: %w", inbound.Tag, err)
		}
		inbound.Sniffing = json_util.RawMessage(sniff)
	}
	return nil
}

func (s *SettingService) applySingBoxAdBlock(cfg *singbox.Config) error {
	policies, err := s.GetAdBlockPolicies()
	if err != nil {
		return err
	}
	if len(policies) > 0 {
		plan, err := s.readAdBlockPlan()
		if err != nil {
			return err
		}
		return applySingBoxPolicyPlan(cfg, plan)
	}
	domains, scope, err := s.adBlockRuntimePolicy()
	if err != nil {
		return err
	}
	if err := stripSingBoxAdBlock(cfg); err != nil {
		return err
	}
	if len(domains) == 0 || (scope.InboundMode == "include" && len(scope.Inbounds) == 0) || (scope.ClientMode == "include" && len(scope.Clients) == 0) {
		return nil
	}
	if cfg.Route == nil {
		cfg.Route = map[string]any{}
	}
	rules, err := nativeRuleArray(cfg.Route["rules"])
	if err != nil {
		return err
	}
	sets, err := nativeRuleArray(cfg.Route["rule_set"])
	if err != nil {
		return err
	}
	var exact, suffix []string
	for _, domain := range domains {
		if strings.HasPrefix(domain, "domain:") {
			suffix = append(suffix, strings.TrimPrefix(domain, "domain:"))
		} else {
			exact = append(exact, domain)
		}
	}
	match := map[string]any{}
	if len(exact) > 0 {
		match["domain"] = exact
	}
	if len(suffix) > 0 {
		match["domain_suffix"] = suffix
	}
	// The named inline set keeps managed data identifiable in native editors,
	// so saving the generated config cannot make the filter permanent.
	cfg.Route["rule_set"] = append(sets, map[string]any{
		"type": "inline", "tag": adblock.OutboundTag, "rules": []map[string]any{match},
	})
	cfg.Route["rules"] = append([]map[string]any{
		{"action": "sniff"},
		singBoxAdBlockScope(map[string]any{"action": "reject", "rule_set": []string{adblock.OutboundTag}}, scope),
	}, rules...)
	return nil
}

func nativeRuleArray(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	if rules, ok := raw.([]map[string]any); ok {
		return rules, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var rules []map[string]any
	err = json.Unmarshal(data, &rules)
	return rules, err
}

func managedSingBoxRule(rule map[string]any) bool {
	if nested, err := nativeRuleArray(rule["rules"]); err == nil {
		for _, child := range nested {
			if managedSingBoxRule(child) {
				return true
			}
		}
	}
	if rule["outbound"] == adblock.OutboundTag {
		return true
	}
	switch refs := rule["rule_set"].(type) {
	case string:
		return refs == adblock.OutboundTag || strings.HasPrefix(refs, adblock.OutboundTag+"-policy-")
	case []string:
		return len(refs) == 1 && managedAdBlockSet(refs[0])
	case []any:
		return len(refs) == 1 && managedAdBlockSet(refs[0])
	}
	return false
}

func stripSingBoxAdBlock(cfg *singbox.Config) error {
	if cfg.Route != nil {
		rules, err := nativeRuleArray(cfg.Route["rules"])
		if err != nil {
			return err
		}
		filtered := make([]map[string]any, 0, len(rules))
		for i, rule := range rules {
			if managedSingBoxRule(rule) {
				continue
			}
			if len(rule) == 1 && rule["action"] == "sniff" && i+1 < len(rules) && managedSingBoxRule(rules[i+1]) {
				continue
			}
			filtered = append(filtered, rule)
		}
		if _, exists := cfg.Route["rules"]; exists {
			cfg.Route["rules"] = filtered
		}
		sets, err := nativeRuleArray(cfg.Route["rule_set"])
		if err != nil {
			return err
		}
		filteredSets := make([]map[string]any, 0, len(sets))
		for _, set := range sets {
			if !managedAdBlockSet(set["tag"]) {
				filteredSets = append(filteredSets, set)
			}
		}
		if _, exists := cfg.Route["rule_set"]; exists {
			cfg.Route["rule_set"] = filteredSets
		}
	}
	filtered := make([]map[string]any, 0, len(cfg.Outbounds))
	for _, outbound := range cfg.Outbounds {
		if outbound["tag"] == adblock.OutboundTag && outbound["type"] == "block" {
			continue
		}
		filtered = append(filtered, outbound)
	}
	cfg.Outbounds = filtered
	return nil
}

func managedAdBlockSet(value any) bool {
	tag, _ := value.(string)
	return tag == adblock.OutboundTag || strings.HasPrefix(tag, adblock.OutboundTag+"-policy-")
}
