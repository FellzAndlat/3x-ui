package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

// Reuse the batch's loopback listeners, routing and outbound context, but
// translate the actual proxy configuration for the selected core.
func buildSingBoxBatchTestConfig(source *xray.Config) (*singbox.Config, error) {
	cfg := singbox.NewConfig()
	cfg.Log = map[string]any{"level": "warn"}
	cfg.Outbounds = nil
	var outbounds []map[string]any
	if err := json.Unmarshal(source.OutboundConfigs, &outbounds); err != nil {
		return nil, err
	}
	var routing map[string]any
	if err := json.Unmarshal(source.RouterConfig, &routing); err != nil {
		return nil, err
	}
	var roots []string
	rules, _ := routing["rules"].([]any)
	for _, item := range rules {
		rule, _ := item.(map[string]any)
		if tag, ok := rule["outboundTag"].(string); ok {
			roots = append(roots, tag)
		}
	}
	selected, err := selectProbeOutbounds(outbounds, roots)
	if err != nil {
		return nil, err
	}
	for _, outbound := range selected {
		protocol, _ := outbound["protocol"].(string)
		if normalizedProbeProtocol(protocol) == "dns" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(protocol), "wireguard") {
			endpoint, err := singbox.TranslateXrayWireGuardEndpoint(outbound)
			if err != nil {
				return nil, err
			}
			// Probes must not create kernel interfaces or alter the host's routing.
			endpoint["system"] = false
			cfg.Endpoints = append(cfg.Endpoints, endpoint)
			continue
		}
		translated, err := singbox.TranslateXrayOutbound(outbound)
		if err != nil {
			return nil, err
		}
		cfg.Outbounds = append(cfg.Outbounds, translated)
	}
	for _, inbound := range source.InboundConfigs {
		cfg.Inbounds = append(cfg.Inbounds, map[string]any{
			"type": "socks", "tag": inbound.Tag, "listen": "127.0.0.1", "listen_port": inbound.Port,
		})
	}
	route, err := singbox.TranslateXrayRouting(routing)
	if err != nil {
		return nil, err
	}
	cfg.Route = route
	// Every test listener has an explicit rule, so no implicit "direct" tag
	// is required (and may be owned by a user's proxy).
	delete(cfg.Route, "final")
	return cfg, nil
}

type singBoxBatchProcess struct {
	process *singbox.Process
	config  *singbox.Config
	path    string
}

var newSingBoxBatchProcess = func(cfg *singbox.Config, path string) batchProcess {
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	return &singBoxBatchProcess{process: singbox.NewTestProcess(path), config: cfg, path: path}
}

func (p *singBoxBatchProcess) Start() error {
	data, err := p.config.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(p.path, data, 0600); err != nil {
		return fmt.Errorf("write sing-box test config: %w", err)
	}
	return p.process.Start(context.Background())
}
func (p *singBoxBatchProcess) Stop() error     { return p.process.Stop() }
func (p *singBoxBatchProcess) IsRunning() bool { return p.process.IsRunning() }
func (p *singBoxBatchProcess) GetResult() string {
	if err := p.process.GetErr(); err != nil {
		return err.Error()
	}
	return ""
}

// automaticProbeContext includes only the tested outbounds and their dialer
// dependencies. Unrelated loopbacks/DNS/unsupported protocols must not poison
// an otherwise valid probe batch on the selected core.
func automaticProbeContext(itemsJSON, allJSON string) (string, error) {
	var items, all []map[string]any
	if err := json.Unmarshal([]byte(itemsJSON), &items); err != nil {
		return "", err
	}
	if err := json.Unmarshal([]byte(allJSON), &all); err != nil {
		return "", err
	}
	byTag := make(map[string]map[string]any)
	for _, ob := range all {
		tag, _ := ob["tag"].(string)
		byTag[tag] = ob
	}
	for _, ob := range items {
		tag, _ := ob["tag"].(string)
		byTag[tag] = ob
	}
	var contextOutbounds []map[string]any
	visited := make(map[string]bool)
	var add func(string)
	add = func(tag string) {
		if visited[tag] {
			return
		}
		visited[tag] = true
		ob, ok := byTag[tag]
		if !ok {
			return
		}
		contextOutbounds = append(contextOutbounds, ob)
		for _, dependency := range probeOutboundDependencies(ob) {
			add(dependency)
		}
		if proxy, ok := ob["proxySettings"].(map[string]any); ok {
			if dependency, ok := proxy["tag"].(string); ok {
				add(dependency)
			}
		}
	}
	for _, ob := range items {
		tag, _ := ob["tag"].(string)
		add(tag)
	}
	data, err := json.Marshal(contextOutbounds)
	return string(data), err
}

func (s *OutboundService) TestAutomaticOutbounds(items, target, all, mode, core string) ([]*TestOutboundResult, error) {
	contextJSON, err := automaticProbeContext(items, all)
	if err != nil {
		return nil, err
	}
	probe := &OutboundService{CoreType: core}
	return probe.TestOutbounds(items, target, contextJSON, mode)
}
