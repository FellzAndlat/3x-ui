package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

type adBlockPlan struct {
	Enabled  bool
	Scope    AdBlockScope
	Base     []string
	Policies []AdBlockPolicy
	Domains  map[string][]string
}

func adBlockPlanFromValues(v map[string]string) (adBlockPlan, error) {
	return adBlockPlanAt(v, time.Now())
}
func adBlockPlanAt(v map[string]string, now time.Time) (adBlockPlan, error) {
	plan := adBlockPlan{Domains: map[string][]string{}}
	if err := json.Unmarshal([]byte(v["adBlockScope"]), &plan.Scope); err != nil {
		return plan, err
	}
	if err := json.Unmarshal([]byte(v["adBlockPolicies"]), &plan.Policies); err != nil {
		return plan, err
	}
	plan.Enabled = v["adBlockEnable"] == "true" && !adBlockPaused(v["adBlockPausedUntil"], now)
	if !plan.Enabled {
		return plan, nil
	}
	var err error
	plan.Base, err = effectiveAdBlockDomains(v["adBlockDomains"], v["adBlockAllowlist"], v["adBlockYoutubeMode"])
	if err != nil {
		return plan, err
	}
	raw := map[string]string{}
	if err := json.Unmarshal([]byte(v["adBlockPolicyDomains"]), &raw); err != nil {
		return plan, err
	}
	for _, p := range plan.Policies {
		if p.Enabled && p.Profile != "off" {
			text, ok := raw[p.ID]
			if !ok {
				return plan, fmt.Errorf("missing validated list for policy %s", p.Name)
			}
			plan.Domains[p.ID], err = effectiveAdBlockDomains(text, v["adBlockAllowlist"], v["adBlockYoutubeMode"])
			if err != nil {
				return plan, err
			}
		}
	}
	return plan, nil
}
func (s *SettingService) readAdBlockPlan() (adBlockPlan, error) {
	keys := []string{"adBlockEnable", "adBlockScope", "adBlockDomains", "adBlockAllowlist", "adBlockYoutubeMode", "adBlockPausedUntil", "adBlockPolicies", "adBlockPolicyDomains"}
	var rows []model.Setting
	if err := database.GetDB().Where("key IN ?", keys).Order("id DESC").Find(&rows).Error; err != nil {
		return adBlockPlan{}, err
	}
	v := map[string]string{}
	for _, key := range keys {
		v[key] = defaultValueMap[key]
	}
	for _, row := range rows {
		v[row.Key] = row.Value
	}
	return adBlockPlanFromValues(v)
}
func planDomains(plan adBlockPlan, id string) []string {
	if id == "" {
		return plan.Base
	}
	return plan.Domains[id]
}
func planPolicyID(plan adBlockPlan, inbound, client string) (string, bool) {
	if !plan.Enabled || !scopeMatches(plan.Scope, inbound, client) {
		return "", false
	}
	if p := selectedAdBlockPolicy(plan.Policies, inbound, client); p != nil {
		return p.ID, p.Profile != "off"
	}
	return "", true
}
func xrayInboundIdentities(in xray.InboundConfig) ([]string, error) {
	var raw map[string]json.RawMessage
	if len(in.Settings) > 0 {
		if err := json.Unmarshal(in.Settings, &raw); err != nil {
			return nil, err
		}
	}
	emails := []string{}
	for _, key := range []string{"clients", "accounts", "users"} {
		if len(raw[key]) == 0 {
			continue
		}
		var users []map[string]any
		if err := json.Unmarshal(raw[key], &users); err != nil {
			return nil, err
		}
		for _, user := range users {
			email, _ := user["email"].(string)
			if email == "" {
				return nil, fmt.Errorf("AdBlock profiles by client require email identities on inbound %s; use inbound profiles for anonymous clients", in.Tag)
			}
			emails = append(emails, email)
		}
	}
	if len(emails) == 0 {
		return []string{""}, nil
	}
	return emails, nil
}
func applyXrayPolicyPlan(cfg *xray.Config, plan adBlockPlan) error {
	router, outbounds, err := adblock.ApplyXrayRouting(cfg.RouterConfig, cfg.OutboundConfigs, nil)
	if err != nil {
		return err
	}
	cfg.RouterConfig, cfg.OutboundConfigs = router, outbounds
	if !plan.Enabled {
		return nil
	}
	needsClients := plan.Scope.ClientMode != "all"
	for _, p := range plan.Policies {
		needsClients = needsClients || p.Enabled && p.Scope.ClientMode != "all"
	}
	type group struct {
		ID              string
		Inbounds, Users []string
	}
	groups := []group{}
	indices := map[string]int{}
	for _, in := range cfg.InboundConfigs {
		if in.Tag == "api" || in.Tag == PanelEgressInboundTag || !scopeContains(plan.Scope.InboundMode, plan.Scope.Inbounds, in.Tag) {
			continue
		}
		identities := []string{""}
		if needsClients {
			identities, err = xrayInboundIdentities(in)
			if err != nil {
				return err
			}
		}
		assignments := map[string][]string{}
		for _, email := range identities {
			id, eligible := planPolicyID(plan, in.Tag, email)
			if eligible && len(planDomains(plan, id)) > 0 {
				assignments[id] = append(assignments[id], email)
			}
		}
		ids := make([]string, 0, len(assignments))
		for id := range assignments {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			users := assignments[id]
			sort.Strings(users)
			key := id + "\x00" + strings.Join(users, "\x00")
			if index, ok := indices[key]; ok {
				groups[index].Inbounds = append(groups[index].Inbounds, in.Tag)
			} else {
				indices[key] = len(groups)
				groups = append(groups, group{id, []string{in.Tag}, users})
			}
		}
	}
	if len(groups) == 0 {
		return nil
	}
	if len(groups) > 32 {
		return fmt.Errorf("AdBlock policy scope produces more than 32 distinct Xray identity groups; simplify inbound/client selection")
	}
	var routing map[string]json.RawMessage
	if err := json.Unmarshal(cfg.RouterConfig, &routing); err != nil {
		return err
	}
	var existing []map[string]any
	if err := json.Unmarshal(routing["rules"], &existing); err != nil {
		return err
	}
	managed := []map[string]any{}
	matchCount := 0
	for _, g := range groups {
		domains := planDomains(plan, g.ID)
		matchCount += len(domains)
		if matchCount > 4*adblock.DefaultMaxDomains {
			return fmt.Errorf("AdBlock policy configuration exceeds 1,200,000 domain matchers")
		}
		xrayDomains := make([]string, 0, len(domains))
		for _, domain := range domains {
			if !strings.HasPrefix(domain, "domain:") {
				domain = "full:" + domain
			}
			xrayDomains = append(xrayDomains, domain)
		}
		rule := map[string]any{"type": "field", "domain": xrayDomains, "inboundTag": g.Inbounds, "outboundTag": adblock.OutboundTag}
		if !(len(g.Users) == 1 && g.Users[0] == "") {
			users := make([]string, len(g.Users))
			for i, email := range g.Users {
				if strings.HasPrefix(email, "regexp:") {
					email = "regexp:^" + regexp.QuoteMeta(email) + "$"
				}
				users[i] = email
			}
			rule["user"] = users
		}
		managed = append(managed, rule)
	}
	// Reuse the standard outbound builder while replacing its generic block rule.
	_, cfg.OutboundConfigs, err = adblock.ApplyXrayRouting(cfg.RouterConfig, cfg.OutboundConfigs, []string{"managed.invalid"})
	if err != nil {
		return err
	}
	routing["rules"], err = json.Marshal(append(managed, existing...))
	if err != nil {
		return err
	}
	raw, err := json.Marshal(routing)
	cfg.RouterConfig = json_util.RawMessage(raw)
	if err != nil {
		return err
	}
	for i := range cfg.InboundConfigs {
		in := &cfg.InboundConfigs[i]
		if in.Tag == "api" || in.Tag == PanelEgressInboundTag || !scopeContains(plan.Scope.InboundMode, plan.Scope.Inbounds, in.Tag) {
			continue
		}
		sniff, err := adblock.XraySniffing(in.Sniffing)
		if err != nil {
			return err
		}
		in.Sniffing = json_util.RawMessage(sniff)
	}
	return nil
}
func scopePredicate(scope AdBlockScope) (map[string]any, bool) {
	conditions := []map[string]any{}
	for _, entry := range []struct {
		Mode, Key string
		Items     []string
	}{{scope.InboundMode, "inbound", scope.Inbounds}, {scope.ClientMode, "auth_user", scope.Clients}} {
		if entry.Mode == "all" {
			continue
		}
		if len(entry.Items) == 0 {
			if entry.Mode == "include" {
				return nil, true
			}
			continue
		}
		condition := map[string]any{entry.Key: entry.Items}
		if entry.Mode == "exclude" {
			condition["invert"] = true
		}
		conditions = append(conditions, condition)
	}
	if len(conditions) == 0 {
		return nil, false
	}
	if len(conditions) == 1 {
		return conditions[0], false
	}
	return map[string]any{"type": "logical", "mode": "and", "rules": conditions}, false
}
func invertedPredicate(predicate map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range predicate {
		out[key] = value
	}
	invert, _ := out["invert"].(bool)
	out["invert"] = !invert
	return out
}
func domainSetMatch(domains []string) map[string]any {
	exact, suffix := []string{}, []string{}
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
	return match
}
func applySingBoxPolicyPlan(cfg *singbox.Config, plan adBlockPlan) error {
	if err := stripSingBoxAdBlock(cfg); err != nil {
		return err
	}
	if !plan.Enabled {
		return nil
	}
	master, never := scopePredicate(plan.Scope)
	if never {
		return nil
	}
	if cfg.Route == nil {
		cfg.Route = map[string]any{}
	}
	existing, err := nativeRuleArray(cfg.Route["rules"])
	if err != nil {
		return err
	}
	sets, err := nativeRuleArray(cfg.Route["rule_set"])
	if err != nil {
		return err
	}
	prior := []map[string]any{}
	managed := []map[string]any{}
	allCovered := false
	appendRule := func(tag string, domains []string, predicate map[string]any) {
		if len(domains) == 0 {
			return
		}
		conditions := []map[string]any{{"rule_set": []string{tag}}}
		if master != nil {
			conditions = append(conditions, master)
		}
		if predicate != nil {
			conditions = append(conditions, predicate)
		}
		for _, previous := range prior {
			conditions = append(conditions, invertedPredicate(previous))
		}
		rule := map[string]any{"type": "logical", "mode": "and", "rules": conditions, "action": "reject"}
		managed = append(managed, rule)
		sets = append(sets, map[string]any{"type": "inline", "tag": tag, "rules": []map[string]any{domainSetMatch(domains)}})
	}
	for _, p := range plan.Policies {
		if !p.Enabled {
			continue
		}
		predicate, never := scopePredicate(p.Scope)
		if never {
			continue
		}
		if p.Profile != "off" {
			appendRule(adblock.OutboundTag+"-policy-"+p.ID, plan.Domains[p.ID], predicate)
		}
		if predicate == nil {
			allCovered = true
			break
		}
		prior = append(prior, predicate)
	}
	if !allCovered {
		appendRule(adblock.OutboundTag, plan.Base, nil)
	}
	if len(managed) == 0 {
		return nil
	}
	cfg.Route["rule_set"] = sets
	cfg.Route["rules"] = append(append([]map[string]any{{"action": "sniff"}}, managed...), existing...)
	return nil
}
