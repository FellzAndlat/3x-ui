package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func autoOutboundTag(id int) string { return fmt.Sprintf("sub-auto-%d", id) }
func autoBlockedTag(id int) string  { return fmt.Sprintf("sub-auto-block-%d", id) }
func autoInboundTag(id int) string  { return fmt.Sprintf("sub-auto-route-%d", id) }

func automaticAliasID(ob map[string]any) (int, bool) {
	tag, _ := ob["tag"].(string)
	if ob["protocol"] != "loopback" || !strings.HasPrefix(tag, "sub-auto-") {
		return 0, false
	}
	id, err := strconv.Atoi(strings.TrimPrefix(tag, "sub-auto-"))
	if err != nil || id <= 0 {
		return 0, false
	}
	settings, _ := ob["settings"].(map[string]any)
	return id, settings["inboundTag"] == autoInboundTag(id)
}

func effectiveAutoTarget(sub *model.OutboundSubscription, members []any) string {
	selected := sub.SelectedTag
	if selected == "" && sub.LastProbe == 0 && sub.LastHealth == 0 && len(members) > 0 {
		if first, ok := members[0].(map[string]any); ok {
			selected, _ = first["tag"].(string)
		}
	}
	for _, raw := range members {
		if ob, ok := raw.(map[string]any); ok && ob["tag"] == selected && selected != "" {
			return selected
		}
	}
	return autoBlockedTag(sub.Id)
}

func autoRouteTarget(sub *model.OutboundSubscription) string {
	var members []any
	_ = json.Unmarshal([]byte(sub.LastFetchedOutbounds), &members)
	members = filterSubscriptionOutbounds("automatic route", members)
	return effectiveAutoTarget(sub, members)
}

// The stable loopback handler survives selection changes. Only its first
// routing rule changes, so Xray's existing hot-update engine preserves streams.
func injectAutomaticOutboundRoutes(cfg *xray.Config) error {
	subs, err := (&OutboundSubscriptionService{}).activeAutomaticSubscriptions()
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}
	var routing map[string]any
	if len(cfg.RouterConfig) > 0 {
		if err := json.Unmarshal(cfg.RouterConfig, &routing); err != nil {
			return err
		}
	}
	if routing == nil {
		routing = map[string]any{}
	}
	rules, _ := routing["rules"].([]any)
	injected := make([]any, 0, len(subs)+len(rules))
	for _, sub := range subs {
		injected = append(injected, map[string]any{"type": "field", "inboundTag": []string{autoInboundTag(sub.Id)}, "outboundTag": autoRouteTarget(sub)})
	}
	routing["rules"] = append(injected, rules...)
	data, err := json.Marshal(routing)
	if err != nil {
		return err
	}
	cfg.RouterConfig = json_util.RawMessage(data)
	return nil
}

func automaticSingBoxSelector(sub *model.OutboundSubscription, members []any) map[string]any {
	tags := []string{autoBlockedTag(sub.Id)}
	for _, raw := range members {
		if ob, ok := raw.(map[string]any); ok {
			if tag, ok := ob["tag"].(string); ok && tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	return map[string]any{"type": "selector", "tag": autoOutboundTag(sub.Id), "outbounds": tags, "default": effectiveAutoTarget(sub, members), "interrupt_exist_connections": false}
}

func ensureAutomaticClashAPI(cfg *singbox.Config) {
	hasSelector := false
	for _, ob := range cfg.Outbounds {
		tag, _ := ob["tag"].(string)
		if ob["type"] == "selector" && strings.HasPrefix(tag, "sub-auto-") {
			hasSelector = true
			break
		}
	}
	if !hasSelector {
		return
	}
	if cfg.Experimental == nil {
		cfg.Experimental = map[string]any{}
	}
	api, _ := cfg.Experimental["clash_api"].(map[string]any)
	if api == nil {
		api = map[string]any{}
	}
	if value, _ := api["external_controller"].(string); value == "" {
		api["external_controller"] = "127.0.0.1:10090"
	}
	cfg.Experimental["clash_api"] = api
}
