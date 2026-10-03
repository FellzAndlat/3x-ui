package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

type AdBlockScopeOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
type AdBlockScopeOptions struct {
	Inbounds []AdBlockScopeOption `json:"inbounds"`
	Clients  []AdBlockScopeOption `json:"clients"`
}

func (s *SettingService) GetAdBlockScopeOptions() (*AdBlockScopeOptions, error) {
	var inbounds []model.Inbound
	if err := database.GetDB().Select("tag", "remark", "settings").Order("id").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	out := &AdBlockScopeOptions{Inbounds: []AdBlockScopeOption{}, Clients: []AdBlockScopeOption{}}
	seen := map[string]bool{}
	for _, in := range inbounds {
		label := in.Tag
		if in.Remark != "" {
			label = in.Remark + " (" + in.Tag + ")"
		}
		out.Inbounds = append(out.Inbounds, AdBlockScopeOption{in.Tag, label})
		var settings struct {
			Clients []struct {
				Email string `json:"email"`
			} `json:"clients"`
		}
		if err := json.Unmarshal([]byte(in.Settings), &settings); err != nil {
			continue
		}
		for _, client := range settings.Clients {
			if client.Email != "" && !seen[client.Email] {
				seen[client.Email] = true
				out.Clients = append(out.Clients, AdBlockScopeOption{client.Email, client.Email})
			}
		}
	}
	sort.Slice(out.Clients, func(i, j int) bool { return out.Clients[i].Value < out.Clients[j].Value })
	return out, nil
}

// Xray has no negated user matcher. Enumerate remaining authenticated users
// from the actual generated config; no "direct" bypass can override routing.
func xrayAdBlockScopeRules(cfg *xray.Config, scope AdBlockScope) ([]map[string]any, error) {
	if scope.InboundMode == "all" && scope.ClientMode == "all" {
		return nil, nil
	}
	rules := []map[string]any{}
	for _, in := range cfg.InboundConfigs {
		if in.Tag == "api" || in.Tag == PanelEgressInboundTag || !scopeContains(scope.InboundMode, scope.Inbounds, in.Tag) {
			continue
		}
		rule := map[string]any{"inboundTag": []string{in.Tag}}
		if scope.ClientMode == "include" {
			if len(scope.Clients) == 0 {
				continue
			}
			rule["user"] = scope.Clients
		} else if scope.ClientMode == "exclude" && len(scope.Clients) > 0 {
			var settings map[string]json.RawMessage
			if len(in.Settings) > 0 {
				if err := json.Unmarshal(in.Settings, &settings); err != nil {
					return nil, fmt.Errorf("AdBlock inbound %s settings: %w", in.Tag, err)
				}
			}
			users := []string{}
			hasClients := false
			for _, key := range []string{"clients", "accounts", "users"} {
				var clients []map[string]any
				if len(settings[key]) == 0 {
					continue
				}
				if err := json.Unmarshal(settings[key], &clients); err != nil {
					return nil, err
				}
				for _, client := range clients {
					hasClients = true
					email, _ := client["email"].(string)
					if email == "" {
						return nil, fmt.Errorf("AdBlock client exclusions require email identities on inbound %s; use inbound exclusions for anonymous clients", in.Tag)
					}
					if scopeContains("exclude", scope.Clients, email) {
						users = append(users, email)
					}
				}
			}
			if hasClients {
				if len(users) == 0 {
					continue
				}
				rule["user"] = users
			}
		}
		rules = append(rules, rule)
	}
	// Merge equivalent client conditions across inbounds. Large domain lists
	// must not be duplicated once per inbound.
	anonymousTags, namedTags := []string{}, []string{}
	users := map[string]bool{}
	for _, rule := range rules {
		tags := rule["inboundTag"].([]string)
		if names, ok := rule["user"].([]string); ok {
			namedTags = append(namedTags, tags...)
			for _, name := range names {
				users[name] = true
			}
		} else {
			anonymousTags = append(anonymousTags, tags...)
		}
	}
	merged := []map[string]any{}
	if len(anonymousTags) > 0 {
		merged = append(merged, map[string]any{"inboundTag": anonymousTags})
	}
	if len(namedTags) > 0 {
		names := make([]string, 0, len(users))
		for name := range users {
			if strings.HasPrefix(name, "regexp:") {
				name = "regexp:^" + regexp.QuoteMeta(name) + "$"
			}
			names = append(names, name)
		}
		sort.Strings(names)
		merged = append(merged, map[string]any{"inboundTag": namedTags, "user": names})
	}
	return merged, nil
}
func applyXrayAdBlockScope(cfg *xray.Config, scope AdBlockScope) error {
	scopeRules, err := xrayAdBlockScopeRules(cfg, scope)
	if err != nil {
		return err
	}
	if scopeRules == nil {
		return nil
	}
	var routing map[string]json.RawMessage
	if err := json.Unmarshal(cfg.RouterConfig, &routing); err != nil {
		return err
	}
	var rules []map[string]any
	if err := json.Unmarshal(routing["rules"], &rules); err != nil {
		return err
	}
	result := []map[string]any{}
	for _, rule := range rules {
		if rule["outboundTag"] != adblock.OutboundTag {
			result = append(result, rule)
			continue
		}
		for _, match := range scopeRules {
			scoped := make(map[string]any, len(rule)+len(match))
			for k, v := range rule {
				scoped[k] = v
			}
			for k, v := range match {
				scoped[k] = v
			}
			result = append(result, scoped)
		}
	}
	routing["rules"], err = json.Marshal(result)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(routing)
	cfg.RouterConfig = json_util.RawMessage(raw)
	return err
}
func singBoxAdBlockScope(rule map[string]any, scope AdBlockScope) map[string]any {
	conditions := []map[string]any{rule}
	for _, entry := range []struct {
		Mode, Key string
		Items     []string
	}{{scope.InboundMode, "inbound", scope.Inbounds}, {scope.ClientMode, "auth_user", scope.Clients}} {
		if entry.Mode == "all" {
			continue
		}
		if len(entry.Items) == 0 {
			continue
		}
		condition := map[string]any{entry.Key: entry.Items}
		if entry.Mode == "exclude" {
			condition["invert"] = true
		}
		conditions = append(conditions, condition)
	}
	if len(conditions) == 1 {
		return rule
	}
	delete(rule, "action")
	return map[string]any{"type": "logical", "mode": "and", "rules": conditions, "action": "reject"}
}
