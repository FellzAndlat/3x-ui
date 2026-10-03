package service

import (
	"encoding/json"
	"fmt"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service/outbound"
	"testing"
	"time"
)

func setupTimedBalancer(t *testing.T) (*model.OutboundSubscription, *int64) {
	t.Helper()
	setupSettingTestDB(t)
	now := int64(1800000000)
	oldClock, oldProbe := automaticBalanceNow, autoBalanceProbe
	automaticBalanceNow = func() time.Time { return time.Unix(now, 0) }
	t.Cleanup(func() { automaticBalanceNow = oldClock; autoBalanceProbe = oldProbe })
	sub := &model.OutboundSubscription{Enabled: true, AutoBalance: true, BalanceMode: "speed", ProbeInterval: 3600, HealthInterval: 30, SwitchInterval: 900,
		SelectedTag: "a", LastProbe: now, LastHealth: now, LastSwitch: now,
		ProbeResults:         `[{"tag":"a","success":true,"httpStatus":200,"downloadMbps":100},{"tag":"b","success":true,"httpStatus":200,"downloadMbps":20}]`,
		LastFetchedOutbounds: `[{"tag":"a","protocol":"socks","settings":{"servers":[{"address":"1.1.1.1","port":1080}]}},{"tag":"b","protocol":"socks","settings":{"servers":[{"address":"1.0.0.1","port":1080}]}}]`}
	if err := database.GetDB().Create(sub).Error; err != nil {
		t.Fatal(err)
	}
	return sub, &now
}

func TestAutoBalancerSwitchesOnTimerEvenWhenCurrentIsFastest(t *testing.T) {
	sub, now := setupTimedBalancer(t)
	calls := 0
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		calls++
		if mode != "real" || target != "" {
			t.Fatalf("scheduled switch ignored speed ranking interval: %s %s", mode, target)
		}
		var batch []map[string]any
		if err := json.Unmarshal([]byte(items), &batch); err != nil {
			t.Fatal(err)
		}
		if len(batch) == 1 {
			return []*outbound.TestOutboundResult{{Tag: "a", Success: true, HTTPStatus: 204, Delay: 10}}, nil
		}
		return []*outbound.TestOutboundResult{{Tag: "a", Success: true, HTTPStatus: 200, Delay: 10, DownloadMbps: 100}, {Tag: "b", Success: true, HTTPStatus: 200, Delay: 50, DownloadMbps: 20}}, nil
	}
	*now += 899
	if n, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || n != 0 {
		t.Fatalf("early switch %d %v", n, err)
	}
	saved, _ := (&OutboundSubscriptionService{}).Get(sub.Id)
	if saved.SelectedTag != "a" {
		t.Fatal(saved.SelectedTag)
	}
	*now++
	if n, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || n != 1 {
		t.Fatalf("timer did not switch %d %v", n, err)
	}
	saved, _ = (&OutboundSubscriptionService{}).Get(sub.Id)
	if saved.SelectedTag != "b" || saved.SwitchReason != "scheduled" || saved.LastSwitch != *now {
		t.Fatalf("%+v", saved)
	}
	before := calls
	if _, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || calls != before {
		t.Fatal("timer repeated early", err, calls)
	}
}

func TestAutoBalancerPingLossDoesNotWaitForRankingOrRotation(t *testing.T) {
	sub, now := setupTimedBalancer(t)
	// A speed file URL must not be used for lightweight health or emergency scans.
	if err := database.GetDB().Model(sub).Update("probe_url", "https://1.1.1.1/large-file").Error; err != nil {
		t.Fatal(err)
	}
	*now += 30
	calls := 0
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		calls++
		if mode != "real" || target != "" {
			t.Fatalf("emergency check downloaded speed data: mode=%s URL=%s", mode, target)
		}
		var members []map[string]any
		if err := json.Unmarshal([]byte(items), &members); err != nil {
			t.Fatal(err)
		}
		if len(members) == 1 && members[0]["tag"] == "a" {
			return []*outbound.TestOutboundResult{{Tag: "a", Error: "timeout"}}, nil
		}
		return []*outbound.TestOutboundResult{{Tag: "a", Error: "timeout"}, {Tag: "b", Success: true, HTTPStatus: 204, Delay: 80}}, nil
	}
	if n, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || n != 1 {
		t.Fatalf("no failover %d %v", n, err)
	}
	saved, _ := (&OutboundSubscriptionService{}).Get(sub.Id)
	if saved.SelectedTag != "b" || saved.SwitchReason != "unreachable" || saved.LastSwitch != *now || saved.LastProbe != sub.LastProbe || calls != 2 {
		t.Fatalf("%+v calls=%d", saved, calls)
	}
}

func TestAutoBalancerFailedAlternativesKeepCurrentAndAdvanceTimer(t *testing.T) {
	sub, now := setupTimedBalancer(t)
	*now += 900
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		return []*outbound.TestOutboundResult{{Tag: "a", Success: true, HTTPStatus: 200, Delay: 10}, {Tag: "b", Error: "timeout"}}, nil
	}
	if n, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
	saved, _ := (&OutboundSubscriptionService{}).Get(sub.Id)
	if saved.SelectedTag != "a" || saved.LastSwitch != *now {
		t.Fatalf("%+v", saved)
	}
}

func TestAutoBalancerPingLossAllDeadThenRecovers(t *testing.T) {
	sub, now := setupTimedBalancer(t)
	*now += 30
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) { return nil, nil }
	if n, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	saved, _ := (&OutboundSubscriptionService{}).Get(sub.Id)
	if saved.SelectedTag != "" || saved.ProbeError == "" || saved.SwitchReason != "unavailable" {
		t.Fatalf("%+v", saved)
	}
	*now += 30
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		return []*outbound.TestOutboundResult{{Tag: "b", Success: true, HTTPStatus: 204, Delay: 15}}, nil
	}
	if n, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || n != 1 {
		t.Fatalf("recovery %d %v", n, err)
	}
	saved, _ = (&OutboundSubscriptionService{}).Get(sub.Id)
	if saved.SelectedTag != "b" || saved.ProbeError != "" {
		t.Fatalf("%+v", saved)
	}
}

func TestAutoBalancerBusyProbeNeverMarksCurrentDead(t *testing.T) {
	sub, now := setupTimedBalancer(t)
	*now += 30
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		return []*outbound.TestOutboundResult{{Tag: "a", Error: "Another outbound test is already running, please wait"}}, nil
	}
	if n, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
	saved, _ := (&OutboundSubscriptionService{}).Get(sub.Id)
	if saved.SelectedTag != "a" || saved.LastHealth != sub.LastHealth {
		t.Fatalf("%+v", saved)
	}
}

func TestAutoBalanceReadyToUsePreset(t *testing.T) {
	setupSettingTestDB(t)
	svc := &OutboundSubscriptionService{}
	sub, err := svc.Create("auto", "https://1.1.1.1/sub", "preset-", "", true, 600, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.AutoBalance || !sub.Prepend || sub.SwitchInterval != 900 || sub.HealthInterval != 30 {
		t.Fatalf("%+v", sub)
	}
	o := &BalanceOptions{Enabled: true, Mode: "speed", Interval: 300}
	if err := o.Validate(false); err != nil || o.URL != defaultAutomaticSpeedURL {
		t.Fatalf("default speed URL missing: %+v %v", o, err)
	}
}

func TestAutoBalancerFailoverDoesNotWaitForEntireSubscription(t *testing.T) {
	sub, now := setupTimedBalancer(t)
	*now += 30
	var members []map[string]any
	if err := json.Unmarshal([]byte(sub.LastFetchedOutbounds), &members); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		ob := map[string]any{"tag": fmt.Sprintf("extra-%d", i), "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": "1.1.1.1", "port": 1080}}}}
		members = append(members, ob)
	}
	encoded, _ := json.Marshal(members)
	if err := database.GetDB().Model(sub).Update("last_fetched_outbounds", string(encoded)).Error; err != nil {
		t.Fatal(err)
	}
	count := 0
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		var batch []map[string]any
		if err := json.Unmarshal([]byte(items), &batch); err != nil {
			t.Fatal(err)
		}
		count += len(batch)
		if len(batch) == 1 && batch[0]["tag"] == "a" {
			return []*outbound.TestOutboundResult{{Tag: "a", Error: "timeout"}}, nil
		}
		return []*outbound.TestOutboundResult{{Tag: "b", Success: true, HTTPStatus: 204, Delay: 30}}, nil
	}
	if n, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	if count != 9 {
		t.Fatalf("probed %d candidates instead of current + first 8 alternatives", count)
	}
	saved, _ := (&OutboundSubscriptionService{}).Get(sub.Id)
	if saved.SelectedTag != "b" {
		t.Fatal(saved.SelectedTag)
	}
}

func TestAutoBalancerScheduledSpeedUsesCachedRanking(t *testing.T) {
	sub, now := setupTimedBalancer(t)
	*now += 900
	var members []map[string]any
	if err := json.Unmarshal([]byte(sub.LastFetchedOutbounds), &members); err != nil {
		t.Fatal(err)
	}
	members = append(members, map[string]any{"tag": "c", "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": "1.1.1.1", "port": 1080}}}})
	encoded, _ := json.Marshal(members)
	if err := database.GetDB().Model(sub).Updates(map[string]any{"last_fetched_outbounds": string(encoded), "probe_results": `[{"tag":"a","success":true,"downloadMbps":100},{"tag":"b","success":true,"downloadMbps":10},{"tag":"c","success":true,"downloadMbps":40}]`}).Error; err != nil {
		t.Fatal(err)
	}
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		if mode != "real" || target != "" {
			t.Fatalf("scheduled switch performed speed download: %s %s", mode, target)
		}
		return []*outbound.TestOutboundResult{{Tag: "a", Success: true, HTTPStatus: 204, Delay: 5}, {Tag: "b", Success: true, HTTPStatus: 204, Delay: 10}, {Tag: "c", Success: true, HTTPStatus: 204, Delay: 100}}, nil
	}
	if n, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	saved, _ := (&OutboundSubscriptionService{}).Get(sub.Id)
	if saved.SelectedTag != "c" || saved.LastProbe != sub.LastProbe {
		t.Fatalf("cached speed ranking not used: %+v", saved)
	}
}

func TestAutoBalancerRankingDownloadsOnlyWhenSpeedTimerDue(t *testing.T) {
	sub, now := setupTimedBalancer(t)
	*now += 3600
	modes := []string{}
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		modes = append(modes, mode)
		if mode == "real" {
			return []*outbound.TestOutboundResult{{Tag: "a", Success: true, HTTPStatus: 204, Delay: 5}}, nil
		}
		if mode != "speed" || target != defaultAutomaticSpeedURL {
			t.Fatalf("%s %s", mode, target)
		}
		return []*outbound.TestOutboundResult{{Tag: "a", Success: true, HTTPStatus: 200, DownloadMbps: 100}, {Tag: "b", Success: true, HTTPStatus: 200, DownloadMbps: 200}}, nil
	}
	if n, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	saved, _ := (&OutboundSubscriptionService{}).Get(sub.Id)
	if saved.SelectedTag != "b" || saved.LastProbe != *now || len(modes) != 2 || modes[0] != "real" || modes[1] != "speed" {
		t.Fatalf("%+v modes=%v", saved, modes)
	}
}

func TestAutoBalancerPingLossInterruptsLongSpeedScan(t *testing.T) {
	sub, now := setupTimedBalancer(t)
	*now += 3600
	var members []map[string]any
	if err := json.Unmarshal([]byte(sub.LastFetchedOutbounds), &members); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		members = append(members, map[string]any{"tag": fmt.Sprintf("node-%d", i), "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": "1.1.1.1", "port": 1080}}}})
	}
	encoded, _ := json.Marshal(members)
	if err := database.GetDB().Model(sub).Update("last_fetched_outbounds", string(encoded)).Error; err != nil {
		t.Fatal(err)
	}
	healthChecks, downloads := 0, 0
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		var batch []map[string]any
		if err := json.Unmarshal([]byte(items), &batch); err != nil {
			t.Fatal(err)
		}
		if mode == "speed" {
			downloads += len(batch)
			return []*outbound.TestOutboundResult{{Tag: "a", Success: true, HTTPStatus: 200, DownloadMbps: 100}, {Tag: "b", Success: true, HTTPStatus: 200, DownloadMbps: 20}}, nil
		}
		if len(batch) == 1 && batch[0]["tag"] == "a" {
			healthChecks++
			if healthChecks == 1 {
				return []*outbound.TestOutboundResult{{Tag: "a", Success: true, HTTPStatus: 204, Delay: 10}}, nil
			}
			return []*outbound.TestOutboundResult{{Tag: "a", Error: "timeout"}}, nil
		}
		return []*outbound.TestOutboundResult{{Tag: "b", Success: true, HTTPStatus: 204, Delay: 50}}, nil
	}
	if n, err := (&OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	saved, _ := (&OutboundSubscriptionService{}).Get(sub.Id)
	if downloads != 2 || saved.SelectedTag != "b" || saved.SwitchReason != "unreachable" || saved.LastProbe != sub.LastProbe {
		t.Fatalf("downloaded %d nodes, selected %+v", downloads, saved)
	}
}

func TestAutoBalancerBrokenSpeedServerKeepsHealthyRoute(t *testing.T) {
	sub, now := setupTimedBalancer(t)
	*now += 3600
	speedCalls := 0
	autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
		if mode == "speed" {
			speedCalls++
			return []*outbound.TestOutboundResult{{Tag: "a", HTTPStatus: 403, Error: "download endpoint refused"}, {Tag: "b", Error: "tiny body"}}, nil
		}
		return []*outbound.TestOutboundResult{{Tag: "a", Success: true, HTTPStatus: 204, Delay: 10}, {Tag: "b", Error: "timeout"}}, nil
	}
	svc := &OutboundSubscriptionService{}
	if _, err := svc.ProbeAutomaticSubscriptions(0); err != nil {
		t.Fatal(err)
	}
	saved, err := svc.Get(sub.Id)
	if err != nil || saved.SelectedTag != "a" || saved.ProbeError == "" || saved.LastProbe != *now || speedCalls != 1 {
		t.Fatalf("%+v calls=%d err=%v", saved, speedCalls, err)
	}
	if _, err = svc.ProbeAutomaticSubscriptions(0); err != nil || speedCalls != 1 {
		t.Fatal("broken endpoint retried without interval", err)
	}
}
func TestAutoBalancerCachedSpeedWithoutCurrentMeasurementKeepsHealthySingleton(t *testing.T) {
	results := []*outbound.TestOutboundResult{{Tag: "a", Success: true, HTTPStatus: 204, Delay: 10}}
	if got := chooseRotatedOutbound(results, "a", "speed"); got != "a" {
		t.Fatal(got)
	}
}
