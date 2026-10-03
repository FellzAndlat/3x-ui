package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock"
	"github.com/SawaMEN/3x-ui/v3/internal/adblock/youtubeproxy"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

const youtubeEgressTag = "youtube-filter-egress"
const youtubeEgressPort = 18081

var youtubeHosts = []string{"youtube.com", "www.youtube.com", "m.youtube.com"}

func (s *SettingService) serverPlan() (AdBlockServerSettings, adBlockPlan, error) {
	settings, err := s.GetAdBlockServer()
	if err != nil {
		return settings, adBlockPlan{}, err
	}
	plan, err := s.readAdBlockPlan()
	if err != nil {
		return settings, plan, err
	}
	plan.Enabled = plan.Enabled && settings.Enabled
	plan.Base = youtubeHosts
	for _, p := range plan.Policies {
		if p.Enabled && p.Profile != "off" {
			plan.Domains[p.ID] = youtubeHosts
		}
	}
	return settings, plan, nil
}
func excludedInfrastructure(tag string) bool {
	return tag == "api" || tag == PanelEgressInboundTag || tag == youtubeEgressTag || strings.HasPrefix(tag, "node-egress")
}
func (s *SettingService) applyXrayYouTubeServer(cfg *xray.Config) error {
	settings, plan, err := s.serverPlan()
	if err != nil || !plan.Enabled {
		return err
	}
	temp := &xray.Config{RouterConfig: json_util.RawMessage(`{"rules":[]}`), OutboundConfigs: json_util.RawMessage(`[]`)}
	for _, in := range cfg.InboundConfigs {
		if !excludedInfrastructure(in.Tag) {
			temp.InboundConfigs = append(temp.InboundConfigs, in)
		}
	}
	if err = applyXrayPolicyPlan(temp, plan); err != nil {
		return err
	}
	var generated struct {
		Rules []map[string]any `json:"rules"`
	}
	if err = json.Unmarshal(temp.RouterConfig, &generated); err != nil {
		return err
	}
	if len(generated.Rules) == 0 {
		return nil
	}
	var route map[string]any
	if err = json.Unmarshal(cfg.RouterConfig, &route); err != nil {
		return err
	}
	options := settings.Limits
	if settings.Outbound != "" {
		if !routingTargetExists(route, cfg.OutboundConfigs, settings.Outbound) {
			return fmt.Errorf("YouTube upstream target %q does not exist", settings.Outbound)
		}
		for _, in := range cfg.InboundConfigs {
			if in.Port == youtubeEgressPort || in.Tag == youtubeEgressTag {
				return fmt.Errorf("YouTube egress bridge conflicts with inbound %s", in.Tag)
			}
		}
		cfg.InboundConfigs = append(cfg.InboundConfigs, xray.InboundConfig{Tag: youtubeEgressTag, Listen: json_util.RawMessage(`"127.0.0.1"`), Port: youtubeEgressPort, Protocol: "socks", Settings: json_util.RawMessage(`{"auth":"noauth","udp":false}`)})
		options.SOCKSAddress = "127.0.0.1:18081"
	}
	port, err := youtubeproxy.Prepare(AdBlockServerCADir(), options)
	if err != nil {
		return err
	}
	var outbounds []map[string]any
	if err = json.Unmarshal(cfg.OutboundConfigs, &outbounds); err != nil {
		return err
	}
	for _, out := range outbounds {
		tag, _ := out["tag"].(string)
		if tag == "youtube-server-filter" || tag == "youtube-quic-block" {
			return fmt.Errorf("remove legacy manual YouTube routing before enabling managed mode")
		}
	}
	outbounds = append(outbounds, map[string]any{"tag": "youtube-server-filter", "protocol": "http", "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": port}}}}, map[string]any{"tag": "youtube-quic-block", "protocol": "blackhole"})
	var rules []any
	rules, _ = route["rules"].([]any)
	managed := []any{}
	if settings.Outbound != "" {
		bridge := map[string]any{"type": "field", "inboundTag": []string{youtubeEgressTag}}
		if routingTagIsBalancer(route, settings.Outbound) {
			bridge["balancerTag"] = settings.Outbound
		} else {
			bridge["outboundTag"] = settings.Outbound
		}
		managed = append(managed, bridge)
	}
	for _, rule := range generated.Rules {
		rule["network"] = "udp"
		rule["port"] = "443"
		rule["outboundTag"] = "youtube-quic-block"
		managed = append(managed, rule)
		tcp := map[string]any{}
		for key, value := range rule {
			tcp[key] = value
		}
		tcp["network"] = "tcp"
		tcp["outboundTag"] = "youtube-server-filter"
		managed = append(managed, tcp)
	}
	// Keep infrastructure/AdBlock rules in front of interception; other ordinary
	// routes are overridden only for explicitly selected YouTube connections.
	index := 0
	for index < len(rules) {
		rule, _ := rules[index].(map[string]any)
		tag, _ := rule["outboundTag"].(string)
		if tag == adblock.OutboundTag || infrastructureScope(rule["inboundTag"]) {
			index++
			continue
		}
		break
	}
	route["rules"] = append(append(append([]any{}, rules[:index]...), managed...), rules[index:]...)
	raw, _ := json.Marshal(route)
	cfg.RouterConfig = json_util.RawMessage(raw)
	raw, _ = json.Marshal(outbounds)
	cfg.OutboundConfigs = json_util.RawMessage(raw)
	for i := range cfg.InboundConfigs {
		in := &cfg.InboundConfigs[i]
		if excludedInfrastructure(in.Tag) || !scopeContains(plan.Scope.InboundMode, plan.Scope.Inbounds, in.Tag) {
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
func replaceDomainSet(rule map[string]any) {
	if _, ok := rule["rule_set"]; ok {
		delete(rule, "rule_set")
		rule["domain"] = youtubeHosts
	}
	if nested, ok := rule["rules"].([]map[string]any); ok {
		for _, r := range nested {
			replaceDomainSet(r)
		}
	}
}
func (s *SettingService) applySingBoxYouTubeServer(cfg *singbox.Config) error {
	settings, plan, err := s.serverPlan()
	if err != nil || !plan.Enabled {
		return err
	}
	temp := singbox.NewConfig()
	if err = applySingBoxPolicyPlan(temp, plan); err != nil {
		return err
	}
	generated, err := nativeRuleArray(temp.Route["rules"])
	if err != nil || len(generated) == 0 {
		return err
	}
	options := settings.Limits
	if settings.Outbound != "" {
		if !singBoxTargetExists(cfg, settings.Outbound) {
			return fmt.Errorf("YouTube upstream %q does not exist", settings.Outbound)
		}
		for _, in := range cfg.Inbounds {
			if singBoxInboundPort(in) == youtubeEgressPort {
				return fmt.Errorf("YouTube bridge port is occupied")
			}
		}
		cfg.Inbounds = append(cfg.Inbounds, map[string]any{"type": "socks", "tag": youtubeEgressTag, "listen": "127.0.0.1", "listen_port": youtubeEgressPort})
		options.SOCKSAddress = "127.0.0.1:18081"
	}
	port, err := youtubeproxy.Prepare(AdBlockServerCADir(), options)
	if err != nil {
		return err
	}
	for _, out := range cfg.Outbounds {
		if out["tag"] == "youtube-server-filter" {
			return fmt.Errorf("remove manual YouTube routing before managed mode")
		}
	}
	cfg.Outbounds = append(cfg.Outbounds, map[string]any{"type": "http", "tag": "youtube-server-filter", "server": "127.0.0.1", "server_port": port})
	existing, err := nativeRuleArray(cfg.Route["rules"])
	if err != nil {
		return err
	}
	infraTags := []string{youtubeEgressTag, PanelEgressInboundTag, "api"}
	for _, in := range cfg.Inbounds {
		tag, _ := in["tag"].(string)
		if excludedInfrastructure(tag) {
			infraTags = append(infraTags, tag)
		}
	}
	managed := []map[string]any{}
	if settings.Outbound != "" {
		managed = append(managed, map[string]any{"inbound": []string{youtubeEgressTag}, "action": "route", "outbound": settings.Outbound})
	}
	for _, rule := range generated {
		if rule["action"] == "sniff" {
			continue
		}
		replaceDomainSet(rule)
		nested := rule["rules"].([]map[string]any)
		nested = append(nested, map[string]any{"port": 443}, map[string]any{"inbound": infraTags, "invert": true})
		rule["rules"] = append(append([]map[string]any{}, nested...), map[string]any{"network": "udp"})
		rule["action"] = "reject"
		managed = append(managed, rule)
		tcp := map[string]any{}
		for k, v := range rule {
			tcp[k] = v
		}
		tcp["rules"] = append(append([]map[string]any{}, nested...), map[string]any{"network": "tcp"})
		tcp["action"] = "route"
		tcp["outbound"] = "youtube-server-filter"
		managed = append(managed, tcp)
	}
	index := 0
	for index < len(existing) {
		rule := existing[index]
		if rule["action"] == "sniff" || rule["action"] == "reject" || infrastructureScope(rule["inbound"]) {
			index++
			continue
		}
		break
	}
	if index == 0 {
		managed = append([]map[string]any{{"action": "sniff"}}, managed...)
	}
	cfg.Route["rules"] = append(append(append([]map[string]any{}, existing[:index]...), managed...), existing[index:]...)
	return nil
}

func infrastructureScope(value any) bool {
	var tags []string
	switch v := value.(type) {
	case []string:
		tags = v
	case []any:
		for _, x := range v {
			tag, _ := x.(string)
			tags = append(tags, tag)
		}
	}
	if len(tags) == 0 {
		return false
	}
	for _, tag := range tags {
		if !excludedInfrastructure(tag) {
			return false
		}
	}
	return true
}
