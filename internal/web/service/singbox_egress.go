package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
)

func singBoxTargetExists(cfg *singbox.Config, tag string) bool {
	tag = strings.TrimSpace(tag)
	if cfg == nil || tag == "" {
		return false
	}
	for _, outbound := range cfg.Outbounds {
		if strings.TrimSpace(fmt.Sprint(outbound["tag"])) == tag {
			return true
		}
	}
	// Native endpoint-style targets (for example WireGuard) are routeable by tag.
	for _, endpoint := range cfg.Endpoints {
		if strings.TrimSpace(fmt.Sprint(endpoint["tag"])) == tag {
			return true
		}
	}
	return false
}

func singBoxInboundPort(inbound map[string]any) int {
	switch value := inbound["listen_port"].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		port, _ := strconv.Atoi(value.String())
		return port
	case string:
		port, _ := strconv.Atoi(value)
		return port
	default:
		return 0
	}
}

func singBoxRouteRules(route map[string]any) []any {
	if route == nil {
		return nil
	}
	switch rules := route["rules"].(type) {
	case []any:
		return rules
	case []map[string]any:
		out := make([]any, 0, len(rules))
		for _, rule := range rules {
			out = append(out, rule)
		}
		return out
	default:
		return nil
	}
}

func injectSingBoxEgressBridge(cfg *singbox.Config, bridgeTag, outboundTag string, basePort int, label string) bool {
	if cfg == nil || strings.TrimSpace(bridgeTag) == "" || strings.TrimSpace(outboundTag) == "" {
		return false
	}
	if !singBoxTargetExists(cfg, outboundTag) {
		logger.Warning(label, " egress: target tag [", outboundTag, "] not found in sing-box config, skipping bridge")
		return false
	}

	usedPorts := make(map[int]struct{}, len(cfg.Inbounds))
	for _, inbound := range cfg.Inbounds {
		if strings.TrimSpace(fmt.Sprint(inbound["tag"])) == bridgeTag {
			logger.Warning(label, " egress: inbound tag [", bridgeTag, "] already exists in sing-box config, skipping bridge")
			return false
		}
		if port := singBoxInboundPort(inbound); port > 0 {
			usedPorts[port] = struct{}{}
		}
	}
	port := basePort
	for {
		if _, used := usedPorts[port]; !used {
			break
		}
		port++
	}

	if cfg.Route == nil {
		cfg.Route = map[string]any{}
	}
	rules := singBoxRouteRules(cfg.Route)
	rule := map[string]any{
		"inbound":  []string{bridgeTag},
		"action":   "route",
		"outbound": outboundTag,
	}
	// Infrastructure rules must win before user/default routing, just like the
	// Xray node/panel egress bridges.
	cfg.Route["rules"] = append([]any{rule}, rules...)
	cfg.Inbounds = append(cfg.Inbounds, map[string]any{
		"type":        "socks",
		"tag":         bridgeTag,
		"listen":      "127.0.0.1",
		"listen_port": port,
	})
	return true
}

func injectSingBoxNodeEgresses(cfg *singbox.Config, nodes []*model.Node) {
	for _, node := range nodes {
		if node == nil || !node.Enable || strings.TrimSpace(node.OutboundTag) == "" {
			continue
		}
		injectSingBoxEgressBridge(
			cfg,
			NodeEgressInboundTag(node.Id),
			node.OutboundTag,
			nodeEgressBasePort+node.Id,
			fmt.Sprintf("node %d", node.Id),
		)
	}
}

// applySingBoxInfrastructureEgress runs after the editable native template has
// been merged. Inbounds are panel-managed, so applying these bridges last keeps
// a native route/outbound template from erasing the internal proxy path.
func applySingBoxInfrastructureEgress(cfg *singbox.Config) {
	if cfg == nil {
		return
	}
	if egressTag, err := singBoxSettingService.GetPanelOutbound(); err != nil {
		logger.Warning("read panelOutbound setting for sing-box failed:", err)
	} else if strings.TrimSpace(egressTag) != "" {
		injectSingBoxEgressBridge(cfg, PanelEgressInboundTag, egressTag, panelEgressBasePort, "panel")
	}

	nodes, err := (&NodeService{}).GetAll()
	if err != nil {
		logger.Warning("read nodes for sing-box egress injection failed:", err)
		return
	}
	injectSingBoxNodeEgresses(cfg, nodes)
}

// singBoxEgressProxyURL resolves the exact collision-adjusted port from the
// config file the running managed sing-box process uses. Regenerating from DB
// here could choose a different port while an older config is still running.
func singBoxEgressProxyURL(tag string) string {
	if !singBoxProcess.IsRunning() || strings.TrimSpace(tag) == "" {
		return ""
	}
	data, err := os.ReadFile(singbox.GetConfigPath())
	if err != nil {
		logger.Warning("read running sing-box config for egress bridge failed:", err)
		return ""
	}
	var cfg singbox.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		logger.Warning("parse running sing-box config for egress bridge failed:", err)
		return ""
	}
	for _, inbound := range cfg.Inbounds {
		if strings.TrimSpace(fmt.Sprint(inbound["tag"])) != tag ||
			!strings.EqualFold(strings.TrimSpace(fmt.Sprint(inbound["type"])), "socks") {
			continue
		}
		if port := singBoxInboundPort(inbound); port > 0 {
			return fmt.Sprintf("socks5://127.0.0.1:%d", port)
		}
	}
	return ""
}

func xrayEgressProxyURL(tag string) string {
	proc := XrayProcess()
	if proc == nil || !proc.IsRunning() {
		return ""
	}
	cfg := proc.GetConfig()
	if cfg == nil {
		return ""
	}
	for i := range cfg.InboundConfigs {
		if cfg.InboundConfigs[i].Tag == tag {
			return fmt.Sprintf("socks5://127.0.0.1:%d", cfg.InboundConfigs[i].Port)
		}
	}
	return ""
}

// coreEgressProxyURL prefers the selected engine while it is running. The
// other engine is used only when the selected one is stopped, so an accidentally
// lingering process cannot override the operator's active core.
func coreEgressProxyURL(settings *SettingService, tag string) string {
	if settings == nil {
		return ""
	}
	coreType, err := settings.GetCoreType()
	if err != nil {
		coreType = CoreTypeXray
	}
	singBoxRunning := singBoxProcess.IsRunning()
	xrayRunning := false
	if proc := XrayProcess(); proc != nil {
		xrayRunning = proc.IsRunning()
	}

	if coreType == CoreTypeSingBox {
		if singBoxRunning {
			return singBoxEgressProxyURL(tag)
		}
		if xrayRunning {
			return xrayEgressProxyURL(tag)
		}
		return ""
	}
	if xrayRunning {
		return xrayEgressProxyURL(tag)
	}
	if singBoxRunning {
		return singBoxEgressProxyURL(tag)
	}
	return ""
}
