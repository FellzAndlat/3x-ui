package service

import (
	"encoding/json"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func TestAutomaticRoutingSelectionOnlyChangesRules(t *testing.T) {
	setupSettingTestDB(t)
	sub := &model.OutboundSubscription{Enabled: true, AutoBalance: true, SelectedTag: "a", LastProbe: 1, LastFetchedOutbounds: `[{"tag":"a","protocol":"freedom","settings":{}},{"tag":"b","protocol":"freedom","settings":{}}]`}
	if err := database.GetDB().Create(sub).Error; err != nil {
		t.Fatal(err)
	}
	var members []any
	_ = json.Unmarshal([]byte(sub.LastFetchedOutbounds), &members)
	obs, _ := json.Marshal(subscriptionAutoOutbound(sub, members))
	cfg := &xray.Config{OutboundConfigs: json_util.RawMessage(obs), RouterConfig: json_util.RawMessage(`{"domainStrategy":"AsIs","rules":[{"type":"field","inboundTag":["api"],"outboundTag":"api"}]}`)}
	if err := injectAutomaticOutboundRoutes(cfg); err != nil {
		t.Fatal(err)
	}
	original := string(cfg.RouterConfig)
	sub.SelectedTag = "b"
	next, err := updateAutomaticRouting(cfg, []*model.OutboundSubscription{sub})
	if err != nil {
		t.Fatal(err)
	}
	diff, ok := xray.ComputeHotDiff(cfg, next)
	if !ok || len(diff.RoutingConfig) == 0 || len(diff.AddedOutbounds) > 0 || len(diff.RemovedOutboundTags) > 0 || len(diff.AddedInbounds) > 0 {
		t.Fatalf("selection was not routing-only: %+v", diff)
	}
	if string(cfg.RouterConfig) != original {
		t.Fatal("running snapshot mutated before core acknowledgement")
	}
	var routing struct {
		Rules []struct {
			Outbound string `json:"outboundTag"`
		}
	}
	_ = json.Unmarshal(next.RouterConfig, &routing)
	if routing.Rules[0].Outbound != "b" || routing.Rules[1].Outbound != "api" {
		t.Fatalf("%+v", routing)
	}
	sub.SelectedTag = ""
	next, err = updateAutomaticRouting(next, []*model.OutboundSubscription{sub})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(next.RouterConfig, &routing)
	if routing.Rules[0].Outbound != autoBlockedTag(sub.Id) {
		t.Fatal("all dead route failed open")
	}
	sub.SelectedTag = "missing"
	if _, err = updateAutomaticRouting(cfg, []*model.OutboundSubscription{sub}); err != nil {
		t.Fatal("missing selection should route to installed block", err)
	}
	cfg.OutboundConfigs = json_util.RawMessage(`[]`)
	if _, err = updateAutomaticRouting(cfg, []*model.OutboundSubscription{sub}); err == nil {
		t.Fatal("applied nonexistent handlers")
	}
}
func TestAutomaticSelectorAndController(t *testing.T) {
	sub := &model.OutboundSubscription{Id: 7, SelectedTag: "b", LastProbe: 1}
	members := []any{map[string]any{"tag": "a"}, map[string]any{"tag": "b"}}
	selector := automaticSingBoxSelector(sub, members)
	if selector["default"] != "b" || selector["interrupt_exist_connections"] != false {
		t.Fatal(selector)
	}
	sub.SelectedTag = ""
	sub.LastProbe = 0
	sub.LastHealth = 1
	if effectiveAutoTarget(sub, members) != autoBlockedTag(7) {
		t.Fatal("failed initial speed health check fell back to first node")
	}
	cfg := singbox.NewConfig()
	cfg.Outbounds = append(cfg.Outbounds, selector)
	ensureAutomaticClashAPI(cfg)
	api := cfg.Experimental["clash_api"].(map[string]any)
	if api["external_controller"] != "127.0.0.1:10090" {
		t.Fatal(api)
	}
	api["external_controller"] = "127.0.0.1:19090"
	api["secret"] = "preserved"
	ensureAutomaticClashAPI(cfg)
	if api["external_controller"] != "127.0.0.1:19090" || api["secret"] != "preserved" {
		t.Fatal("overwrote custom controller")
	}
}
func TestAutomaticApplyStoppedCoreReportsPending(t *testing.T) {
	sub, _ := setupTimedBalancer(t)
	svc := &OutboundSubscriptionService{}
	if _, err := svc.ApplyAutomaticSelections(); err != nil {
		t.Fatal(err)
	}
	saved, err := svc.Get(sub.Id)
	if err != nil || saved.AppliedTag != "" || saved.ApplyError == "" {
		t.Fatalf("%+v %v", saved, err)
	}
	if n, err := svc.ApplyAutomaticSelections(); err != nil || n != 0 {
		t.Fatalf("unchanged error repeated %d %v", n, err)
	}
}
