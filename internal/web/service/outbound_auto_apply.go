package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

var automaticApplyMu sync.Mutex

// updateAutomaticRouting modifies only already installed loopback routes. It
// never changes unrelated pending panel edits or fabricates missing handlers.
func updateAutomaticRouting(cfg *xray.Config, subs []*model.OutboundSubscription) (*xray.Config, error) {
	var obs []map[string]any
	if err := json.Unmarshal(cfg.OutboundConfigs, &obs); err != nil {
		return nil, err
	}
	tags := map[string]bool{}
	aliases := map[int]bool{}
	for _, ob := range obs {
		tag, _ := ob["tag"].(string)
		tags[tag] = true
		if id, ok := automaticAliasID(ob); ok {
			aliases[id] = true
		}
	}
	var routing map[string]any
	if err := json.Unmarshal(cfg.RouterConfig, &routing); err != nil {
		return nil, err
	}
	rules, _ := routing["rules"].([]any)
	targets := map[string]string{}
	for _, sub := range subs {
		target := autoRouteTarget(sub)
		if !aliases[sub.Id] || !tags[target] {
			return nil, fmt.Errorf("automatic handlers missing; reload the core after subscription changes")
		}
		targets[autoInboundTag(sub.Id)] = target
	}
	found := map[string]bool{}
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		inbound, _ := rule["inboundTag"].([]any)
		if len(inbound) != 1 {
			continue
		}
		tag, _ := inbound[0].(string)
		if target, ok := targets[tag]; ok {
			rule["outboundTag"] = target
			delete(rule, "balancerTag")
			found[tag] = true
		}
	}
	for tag := range targets {
		if !found[tag] {
			return nil, fmt.Errorf("automatic routing missing; reload the core after subscription changes")
		}
	}
	encoded, err := json.Marshal(routing)
	if err != nil {
		return nil, err
	}
	next := *cfg
	next.RouterConfig = json_util.RawMessage(encoded)
	return &next, nil
}
func applyAutomaticXray(subs []*model.OutboundSubscription) (map[int]string, error) {
	lock.Lock()
	defer lock.Unlock()
	applied := map[int]string{}
	process := currentXrayProcess()
	if process == nil || !process.IsRunning() {
		return applied, fmt.Errorf("core is stopped; selection will apply on startup")
	}
	current := process.GetConfig()
	if current == nil {
		return applied, fmt.Errorf("running core configuration unavailable")
	}
	next, err := updateAutomaticRouting(current, subs)
	if err != nil {
		return applied, err
	}
	if process.GetAPIPort() <= 0 {
		return applied, fmt.Errorf("core API is disabled; enable RoutingService for automatic switching")
	}
	api := &xray.XrayAPI{}
	if err := api.Init(process.GetAPIPort()); err != nil {
		return applied, err
	}
	defer api.Close()
	needsApply := !bytes.Equal(next.RouterConfig, current.RouterConfig)
	for _, sub := range subs {
		actual, err := api.TestRoute(xray.RouteTestRequest{InboundTag: autoInboundTag(sub.Id), IP: "1.1.1.1", Port: 443})
		if err != nil {
			return applied, fmt.Errorf("live route verification failed: %w", err)
		}
		if !actual.Matched || actual.OutboundTag != autoRouteTarget(sub) {
			needsApply = true
		}
		applied[sub.Id] = actual.OutboundTag
	}
	if needsApply {
		if err := api.ApplyRoutingConfig(next.RouterConfig); err != nil {
			return applied, fmt.Errorf("live routing update failed: %w", err)
		}
		for _, sub := range subs {
			actual, err := api.TestRoute(xray.RouteTestRequest{InboundTag: autoInboundTag(sub.Id), IP: "1.1.1.1", Port: 443})
			if err != nil {
				return applied, fmt.Errorf("live route verification failed: %w", err)
			}
			applied[sub.Id] = actual.OutboundTag
			if !actual.Matched || actual.OutboundTag != autoRouteTarget(sub) {
				return applied, fmt.Errorf("core did not confirm automatic route")
			}
		}
		process.SetConfig(next)
	}
	return applied, nil
}
func localAutomaticSelectorClient() (*singbox.SelectorClient, error) {
	data, err := os.ReadFile(singbox.GetConfigPath())
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Experimental struct {
			ClashAPI struct {
				Controller string `json:"external_controller"`
				Secret     string `json:"secret"`
			} `json:"clash_api"`
		} `json:"experimental"`
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return singbox.NewSelectorClient(cfg.Experimental.ClashAPI.Controller, cfg.Experimental.ClashAPI.Secret)
}

// ApplyAutomaticSelections retries unconfirmed selections every scheduler
// tick. Database choice and the running core's acknowledgement stay distinct.
func (s *OutboundSubscriptionService) ApplyAutomaticSelections() (int, error) {
	if !automaticApplyMu.TryLock() {
		return 0, nil
	}
	defer automaticApplyMu.Unlock()
	subs, err := s.activeAutomaticSubscriptions()
	if err != nil || len(subs) == 0 {
		return 0, err
	}
	core, err := (&SettingService{}).GetCoreType()
	if err != nil {
		return 0, err
	}
	applied := map[int]string{}
	failures := map[int]error{}
	if core == CoreTypeSingBox {
		if !(&SingBoxService{}).IsRunning() {
			err = fmt.Errorf("core is stopped; selection will apply on startup")
		} else {
			var client *singbox.SelectorClient
			client, err = localAutomaticSelectorClient()
			if err == nil {
				defer client.Close()
				for _, sub := range subs {
					actual, e := client.Select(context.Background(), autoOutboundTag(sub.Id), autoRouteTarget(sub))
					applied[sub.Id] = actual
					failures[sub.Id] = e
				}
			}
		}
	} else {
		applied, err = applyAutomaticXray(subs)
	}
	changes := 0
	for _, sub := range subs {
		message := ""
		failure := failures[sub.Id]
		if err != nil {
			failure = err
		}
		if failure != nil {
			message = failure.Error()
		}
		actual := applied[sub.Id]
		if sub.AppliedTag == actual && sub.ApplyError == message {
			continue
		}
		result := database.GetDB().Model(&model.OutboundSubscription{}).Where("id = ? AND selected_tag = ? AND updated_at = ? AND last_fetched_outbounds = ? AND enabled = ? AND auto_balance = ?", sub.Id, sub.SelectedTag, sub.UpdatedAt, sub.LastFetchedOutbounds, true, true).Updates(map[string]any{"applied_tag": actual, "apply_error": message})
		if result.Error != nil {
			return changes, result.Error
		}
		if result.RowsAffected > 0 {
			changes++
		}
	}
	return changes, nil
}
