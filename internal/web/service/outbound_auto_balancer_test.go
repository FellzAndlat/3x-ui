package service

import (
	"encoding/json"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service/outbound"
	"testing"
)

func TestAutoBalancerSelection(t *testing.T) {
	results := []*outbound.TestOutboundResult{
		{Tag: "a", Success: true, HTTPStatus: 204, Delay: 100, DownloadMbps: 10},
		{Tag: "b", Success: true, HTTPStatus: 200, Delay: 70, DownloadMbps: 10.5},
		{Tag: "c", Success: true, HTTPStatus: 503, Delay: 1, DownloadMbps: 999},
	}
	for _, tc := range []struct {
		name, current, mode, want string
		tolerance                 int
	}{
		{"fastest", "", "latency", "b", 0}, {"retain within tolerance", "a", "latency", "a", 50},
		{"switch beyond tolerance", "a", "latency", "b", 20}, {"speed hysteresis", "a", "speed", "a", 0},
		{"speed initial", "", "speed", "b", 0}, {"dead current", "dead", "latency", "b", 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chooseSubscriptionOutbound(results, tc.current, tc.mode, tc.tolerance); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	results[1].DownloadMbps = 12
	if got := chooseSubscriptionOutbound(results, "a", "speed", 0); got != "b" {
		t.Fatal(got)
	}
	if got := chooseSubscriptionOutbound(nil, "a", "latency", 50); got != "" {
		t.Fatal(got)
	}
}
func TestAutoBalancerAliasFailsClosed(t *testing.T) {
	a := map[string]any{"tag": "a", "protocol": "socks"}
	b := map[string]any{"tag": "b", "protocol": "socks"}
	sub := &model.OutboundSubscription{Id: 7, AutoBalance: true, SelectedTag: "b", LastProbe: 1}
	arr := subscriptionAutoOutbound(sub, []any{a, b})
	alias := arr[0].(map[string]any)
	if alias["tag"] != "sub-auto-7" || alias["protocol"] != "loopback" || b["tag"] != "b" {
		t.Fatalf("%v %v", alias, b)
	}
	sub.SelectedTag = "missing"
	arr = subscriptionAutoOutbound(sub, []any{a, b})
	if effectiveAutoTarget(sub, []any{a, b}) != autoBlockedTag(sub.Id) {
		t.Fatal(arr)
	}
	sub.SelectedTag = ""
	sub.LastProbe = 0
	if effectiveAutoTarget(sub, []any{a}) != "a" {
		t.Fatal("initial fallback missing")
	}
	if empty := subscriptionAutoOutbound(sub, nil); len(empty) != 2 || empty[1].(map[string]any)["protocol"] != "blackhole" {
		t.Fatal(empty)
	}
	sub.AutoBalance = false
	if len(subscriptionAutoOutbound(sub, []any{a})) != 1 {
		t.Fatal("disabled alias")
	}
}
func TestAutoBalanceValidation(t *testing.T) {
	for _, o := range []BalanceOptions{
		{SwitchInterval: 29}, {HealthInterval: 9}, {Enabled: true, Mode: "speed", Interval: 30, URL: "https://1.1.1.1/file"},
		{Mode: "unknown"}, {Interval: 29}, {Tolerance: -1}, {URL: "file:///etc/passwd"}, {URL: "http://127.0.0.1/test"},
	} {
		if o.Validate(false) == nil {
			t.Fatalf("accepted invalid options: %+v", o)
		}
	}
	o := BalanceOptions{}
	if err := o.Validate(false); err != nil || o.Interval != 300 || o.Mode != "latency" {
		t.Fatalf("%+v %v", o, err)
	}
}
func TestAutoBalancerProbePersistenceAndStaleEdit(t *testing.T) {
	setupSettingTestDB(t)
	db := database.GetDB()
	sub := &model.OutboundSubscription{Enabled: true, AutoBalance: true, BalanceMode: "latency", ProbeInterval: 300, LastFetchedOutbounds: `[{"tag":"a","protocol":"socks","settings":{"servers":[{"address":"1.1.1.1","port":1080}]}}]`}
	if err := db.Create(sub).Error; err != nil {
		t.Fatal(err)
	}
	original := autoBalanceProbe
	t.Cleanup(func() { autoBalanceProbe = original })
	calls := 0
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		calls++
		if mode != "real" || core != "xray" {
			t.Fatalf("%s %s", mode, core)
		}
		return []*outbound.TestOutboundResult{{Tag: "a", Success: true, HTTPStatus: 204, Delay: 25}}, nil
	}
	svc := &OutboundSubscriptionService{}
	n, err := svc.ProbeAutomaticSubscriptions(0)
	if err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	saved, err := svc.Get(sub.Id)
	if err != nil || saved.SelectedTag != "a" || saved.LastProbe == 0 {
		t.Fatalf("%+v %v", saved, err)
	}
	var results []outbound.TestOutboundResult
	if err := json.Unmarshal([]byte(saved.ProbeResults), &results); err != nil || len(results) != 1 {
		t.Fatal(saved.ProbeResults)
	}
	if n, err := svc.ProbeAutomaticSubscriptions(0); err != nil || n != 0 || calls != 1 {
		t.Fatalf("%d %v calls=%d", n, err, calls)
	}
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		if err := db.Model(sub).Update("auto_balance", false).Error; err != nil {
			t.Fatal(err)
		}
		return nil, nil
	}
	if n, err := svc.ProbeAutomaticSubscriptions(sub.Id); err != nil || n != 0 {
		t.Fatalf("stale result committed: %d %v", n, err)
	}
	saved, _ = svc.Get(sub.Id)
	if saved.SelectedTag != "a" {
		t.Fatal("stale probe overwrote selection")
	}
}

func TestAutoBalancerSingBoxSkipsIncompatibleMembers(t *testing.T) {
	setupSettingTestDB(t)
	if err := (&SettingService{}).SetCoreType(CoreTypeSingBox); err != nil {
		t.Fatal(err)
	}
	members := []any{
		map[string]any{"tag": "valid", "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": "1.1.1.1", "port": 1080}}}},
		map[string]any{"tag": "unsupported", "protocol": "loopback", "settings": map[string]any{"inboundTag": "inbound"}},
	}
	kept := filterSubscriptionOutbounds("test", members)
	if len(kept) != 1 || kept[0].(map[string]any)["tag"] != "valid" || len(members) != 2 {
		t.Fatalf("%v", kept)
	}
}

func TestOutboundRefreshPreservesConcurrentBalancerEdit(t *testing.T) {
	setupSettingTestDB(t)
	var id int
	id = serveOutboundSubscription(t, "sub-", func(n int) string {
		if err := database.GetDB().Model(&model.OutboundSubscription{}).Where("id = ?", id).
			Updates(map[string]any{"auto_balance": true, "balance_mode": "speed", "selected_tag": "selected"}).Error; err != nil {
			t.Error(err)
		}
		return "vless://00000000-0000-4000-8000-000000000000@1.1.1.1:443?security=tls&type=tcp#node"
	})
	if _, err := (&OutboundSubscriptionService{}).Refresh(id); err != nil {
		t.Fatal(err)
	}
	saved, err := (&OutboundSubscriptionService{}).Get(id)
	if err != nil || !saved.AutoBalance || saved.BalanceMode != "speed" || saved.SelectedTag != "selected" {
		t.Fatalf("%+v %v", saved, err)
	}
}
