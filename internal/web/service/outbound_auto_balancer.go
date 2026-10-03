package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service/outbound"
)

// BalanceOptions are persisted atomically with subscription edits.
type BalanceOptions struct {
	Enabled        bool
	Mode           string
	URL            string
	Interval       int
	Tolerance      int
	SwitchInterval int
	HealthInterval int
}

const defaultAutomaticSpeedURL = "https://speed.cloudflare.com/__down?bytes=1048576"

func (o *BalanceOptions) Validate(allowPrivate bool) error {
	if o.SwitchInterval == 0 {
		o.SwitchInterval = 900
	}
	if o.HealthInterval == 0 {
		o.HealthInterval = 30
	}
	if o.SwitchInterval < 30 || o.SwitchInterval > 86400 {
		return fmt.Errorf("switchInterval must be 30..86400 seconds")
	}
	if o.HealthInterval < 10 || o.HealthInterval > 300 {
		return fmt.Errorf("healthInterval must be 10..300 seconds")
	}
	if o.Mode == "" {
		o.Mode = "latency"
	}
	if o.Mode != "latency" && o.Mode != "speed" {
		return fmt.Errorf("balanceMode must be latency or speed")
	}
	if o.Interval == 0 {
		o.Interval = 300
	}
	if o.Interval < 30 || o.Interval > 86400 {
		return fmt.Errorf("probeInterval must be 30..86400 seconds")
	}
	if o.Tolerance < 0 || o.Tolerance > 10000 {
		return fmt.Errorf("tolerance must be 0..10000 ms")
	}
	if o.Mode == "speed" && o.Enabled {
		if o.Interval < 300 {
			return fmt.Errorf("speed probes require an interval of at least 300 seconds")
		}
		if strings.TrimSpace(o.URL) == "" {
			o.URL = defaultAutomaticSpeedURL
		}
	}
	if o.URL != "" && o.URL != defaultAutomaticSpeedURL {
		clean, err := SanitizePublicHTTPURL(o.URL, allowPrivate)
		if err != nil {
			return err
		}
		o.URL = clean
	}
	return nil
}
func (o *BalanceOptions) apply(sub *model.OutboundSubscription) {
	if sub.AutoBalance != o.Enabled || sub.BalanceMode != o.Mode || sub.ProbeURL != o.URL {
		sub.SelectedTag = ""
		sub.LastSwitch = 0
		sub.SwitchReason = ""
		sub.AppliedTag, sub.ApplyError = "", ""
	}
	sub.AutoBalance, sub.BalanceMode, sub.ProbeURL = o.Enabled, o.Mode, o.URL
	sub.ProbeInterval, sub.Tolerance = o.Interval, o.Tolerance
	sub.SwitchInterval, sub.HealthInterval = o.SwitchInterval, o.HealthInterval
	sub.LastHealth = 0
	sub.LastProbe, sub.ProbeError, sub.ProbeResults = 0, "", ""
}
func subscriptionAutoOutbound(sub *model.OutboundSubscription, members []any) []any {
	if !sub.AutoBalance {
		return members
	}
	alias := map[string]any{"tag": autoOutboundTag(sub.Id), "protocol": "loopback", "settings": map[string]any{"inboundTag": autoInboundTag(sub.Id)}}
	blocked := map[string]any{"tag": autoBlockedTag(sub.Id), "protocol": "blackhole", "settings": map[string]any{}}
	return append([]any{alias, blocked}, members...)
}
func chooseSubscriptionOutbound(results []*outbound.TestOutboundResult, current, mode string, tolerance int) string {
	var best, previous *outbound.TestOutboundResult
	for _, r := range results {
		if r == nil || !r.Success || r.HTTPStatus < 200 || r.HTTPStatus >= 300 {
			continue
		}
		if mode == "speed" && r.DownloadMbps <= 0 {
			continue
		}
		if r.Tag == current {
			previous = r
		}
		if best == nil || (mode == "speed" && r.DownloadMbps > best.DownloadMbps) || (mode != "speed" && r.Delay < best.Delay) {
			best = r
		}
	}
	if best == nil {
		return ""
	}
	if previous != nil {
		if mode == "speed" && best.DownloadMbps <= previous.DownloadMbps*1.1 {
			return current
		}
		if mode != "speed" && previous.Delay <= best.Delay+int64(tolerance) {
			return current
		}
	}
	return best.Tag
}

var autoBalanceMu sync.Mutex
var automaticBalanceNow = time.Now
var autoBalanceProbe = func(items, target, all, mode, core string) ([]*outbound.TestOutboundResult, error) {
	return (&outbound.OutboundService{}).TestAutomaticOutbounds(items, target, all, mode, core)
}

// runAutomaticProbe batches candidates and distinguishes an occupied probe
// engine from failed nodes, so busy manual tests never cause a false failover.
func runAutomaticProbe(members []any, target, all, mode, core string) ([]*outbound.TestOutboundResult, bool, error) {
	var results []*outbound.TestOutboundResult
	for start := 0; start < len(members); start += 50 {
		batch, err := json.Marshal(members[start:min(start+50, len(members))])
		if err != nil {
			return nil, false, err
		}
		r, err := autoBalanceProbe(string(batch), target, all, mode, core)
		if err != nil {
			return nil, false, err
		}
		for _, result := range r {
			if result != nil && strings.Contains(result.Error, "Another outbound test is already running") {
				return nil, true, nil
			}
		}
		results = append(results, r...)
	}
	return results, false, nil
}

// Long speed scans check the live node between small download batches. A
// failure preempts ranking instead of waiting for all downloads to finish.
func runAutomaticSpeedRanking(members, current []any, target, all, core string) ([]*outbound.TestOutboundResult, bool, bool, error) {
	var results []*outbound.TestOutboundResult
	for start := 0; start < len(members); start += 2 {
		r, busy, err := runAutomaticProbe(members[start:min(start+2, len(members))], target, all, "speed", core)
		if err != nil || busy {
			return nil, busy, false, err
		}
		results = append(results, r...)
		if start+2 < len(members) {
			health, busy, err := runAutomaticProbe(current, "", all, "real", core)
			if err != nil || busy {
				return nil, busy, false, err
			}
			if chooseSubscriptionOutbound(health, "", "latency", 0) == "" {
				return nil, false, true, nil
			}
		}
	}
	return results, false, false, nil
}

// Emergency replacement checks small candidate groups and stops at the first
// healthy group. A large subscription must not delay failover until every node
// has timed out. Full ranking remains a separate scheduled operation.
func runAutomaticFailoverProbe(members []any, current, target, all, core string) ([]*outbound.TestOutboundResult, bool, error) {
	var alternatives, previous []any
	for _, raw := range members {
		if ob, ok := raw.(map[string]any); ok && ob["tag"] == current {
			previous = append(previous, raw)
		} else {
			alternatives = append(alternatives, raw)
		}
	}
	var results []*outbound.TestOutboundResult
	for start := 0; start < len(alternatives); start += 8 {
		r, busy, err := runAutomaticProbe(alternatives[start:min(start+8, len(alternatives))], target, all, "real", core)
		if err != nil || busy {
			return nil, busy, err
		}
		results = append(results, r...)
		if chooseSubscriptionOutbound(r, "", "latency", 0) != "" {
			return results, false, nil
		}
	}
	// A transient failure may have recovered when no alternate is available.
	r, busy, err := runAutomaticProbe(previous, target, all, "real", core)
	return append(results, r...), busy, err
}

// chooseRotatedOutbound selects the best healthy alternative even if the
// current node is fastest. With no healthy alternative it retains the current.
func chooseRotatedOutbound(results []*outbound.TestOutboundResult, current, mode string) string {
	alternatives := make([]*outbound.TestOutboundResult, 0, len(results))
	for _, r := range results {
		if r != nil && r.Tag != current {
			alternatives = append(alternatives, r)
		}
	}
	if next := chooseSubscriptionOutbound(alternatives, "", mode, 0); next != "" {
		return next
	}
	// Previously unmeasured alternatives can still be chosen after a health test.
	if mode == "speed" {
		if next := chooseSubscriptionOutbound(alternatives, "", "latency", 0); next != "" {
			return next
		}
	}
	if next := chooseSubscriptionOutbound(results, "", mode, 0); next != "" {
		return next
	}
	if mode == "speed" {
		return chooseSubscriptionOutbound(results, "", "latency", 0)
	}
	return ""
}

// ProbeAutomaticSubscriptions keeps health checks independent of ranking and
// rotation timers. In-flight results are committed only to unchanged settings.
func (s *OutboundSubscriptionService) ProbeAutomaticSubscriptions(forceID int) (int, error) {
	if !autoBalanceMu.TryLock() {
		return 0, fmt.Errorf("automatic outbound check is already running")
	}
	defer autoBalanceMu.Unlock()
	var subs []*model.OutboundSubscription
	if err := database.GetDB().Where("enabled = ? AND auto_balance = ?", true, true).Order("priority asc, id asc").Find(&subs).Error; err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	settings := &SettingService{}
	core, err := settings.GetCoreType()
	if err != nil {
		return 0, err
	}
	template, err := settings.GetXrayConfigTemplate()
	if err != nil {
		return 0, err
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(template), &config); err != nil {
		return 0, err
	}
	all, _ := config["outbounds"].([]any)
	active, err := s.AllActiveOutbounds()
	if err != nil {
		return 0, err
	}
	allJSON, err := json.Marshal(append(all, active...))
	if err != nil {
		return 0, err
	}
	changes := 0
	for _, sub := range subs {
		if forceID > 0 && sub.Id != forceID {
			continue
		}
		now := automaticBalanceNow().Unix()
		interval := sub.ProbeInterval
		if interval < 30 {
			interval = 300
		}
		healthInterval := sub.HealthInterval
		if healthInterval < 10 {
			healthInterval = 30
		}
		switchInterval := sub.SwitchInterval
		if switchInterval < 30 {
			switchInterval = 900
		}
		rankingDue := forceID > 0 || sub.LastProbe == 0 || sub.LastProbe+int64(interval) <= now
		rotationDue := sub.SelectedTag != "" && sub.LastSwitch > 0 && sub.LastSwitch+int64(switchInterval) <= now
		healthDue := sub.LastHealth == 0 || sub.LastHealth+int64(healthInterval) <= now
		if !rankingDue && !rotationDue && !healthDue {
			continue
		}
		var members []any
		if strings.TrimSpace(sub.LastFetchedOutbounds) != "" {
			if err := json.Unmarshal([]byte(sub.LastFetchedOutbounds), &members); err != nil {
				continue
			}
		}
		members = filterSubscriptionOutbounds("auto balancer", members)
		var current []any
		for _, raw := range members {
			if ob, ok := raw.(map[string]any); ok && ob["tag"] == sub.SelectedTag {
				current = append(current, raw)
				break
			}
		}
		unavailable := sub.SelectedTag == "" || len(current) == 0
		updates := map[string]any{}
		// Lightweight health tests never download the speed-test file. A failed
		// current node triggers candidate checks without waiting for either timer.
		if (healthDue || rankingDue || rotationDue) && !unavailable {
			healthURL := sub.ProbeURL
			if sub.BalanceMode == "speed" {
				healthURL = ""
			}
			results, busy, err := runAutomaticProbe(current, healthURL, string(allJSON), "real", core)
			if err != nil {
				return changes, err
			}
			if busy {
				continue
			}
			unavailable = chooseSubscriptionOutbound(results, "", "latency", 0) == ""
			updates["last_health"] = automaticBalanceNow().Unix()
		}
		probeWarning := ""
		chosen := sub.SelectedTag
		reason := sub.SwitchReason
		fullScan := rankingDue || rotationDue || unavailable
		if fullScan {
			mode, selectionMode, target := "real", "latency", sub.ProbeURL
			if sub.BalanceMode == "speed" {
				if rankingDue && !unavailable {
					mode, selectionMode = "speed", "speed"
					if target == "" {
						target = defaultAutomaticSpeedURL
					}
				} else {
					target = ""
				}
			}
			var results []*outbound.TestOutboundResult
			var busy bool
			var err error
			if mode == "speed" {
				var failed bool
				results, busy, failed, err = runAutomaticSpeedRanking(members, current, target, string(allJSON), core)
				if err == nil && !busy && failed {
					unavailable = true
					mode, selectionMode, target = "real", "latency", ""
					results, busy, err = runAutomaticFailoverProbe(members, sub.SelectedTag, target, string(allJSON), core)
				}
			} else if unavailable && sub.SelectedTag != "" {
				results, busy, err = runAutomaticFailoverProbe(members, sub.SelectedTag, target, string(allJSON), core)
			} else {
				results, busy, err = runAutomaticProbe(members, target, string(allJSON), mode, core)
			}
			if err != nil {
				return changes, err
			}
			if busy {
				continue
			}
			// A broken download endpoint is not proof that every proxy is down.
			// Recheck with the small health request and fall back to latency.
			if mode == "speed" && chooseSubscriptionOutbound(results, "", "speed", 0) == "" {
				results, busy, err = runAutomaticProbe(members, "", string(allJSON), "real", core)
				if err != nil {
					return changes, err
				}
				if busy {
					continue
				}
				selectionMode = "latency"
				probeWarning = "Speed test unavailable; selection uses proxy health and latency"
			}
			// Timed rotation must not download speed files more often than the
			// ranking interval. Revalidate availability and reuse the last speed sample.
			if sub.BalanceMode == "speed" && rotationDue && !rankingDue && !unavailable {
				var cached []*outbound.TestOutboundResult
				if json.Unmarshal([]byte(sub.ProbeResults), &cached) == nil {
					speeds := make(map[string]float64)
					for _, r := range cached {
						if r != nil && r.Success && r.DownloadMbps > 0 {
							speeds[r.Tag] = r.DownloadMbps
						}
					}
					if len(speeds) > 0 {
						selectionMode = "speed"
						for _, r := range results {
							if r != nil {
								r.DownloadMbps = speeds[r.Tag]
							}
						}
					}
				}
			}
			chosen = chooseSubscriptionOutbound(results, sub.SelectedTag, selectionMode, sub.Tolerance)
			if rotationDue || unavailable {
				chosen = chooseRotatedOutbound(results, sub.SelectedTag, selectionMode)
			}
			encoded, _ := json.Marshal(results)
			updates["probe_results"] = string(encoded)
			updates["last_health"] = automaticBalanceNow().Unix()
			if rankingDue && (sub.BalanceMode != "speed" || mode == "speed") {
				updates["last_probe"] = automaticBalanceNow().Unix()
			}
			if chosen != sub.SelectedTag {
				switch {
				case chosen == "":
					reason = "unavailable"
				case sub.SelectedTag == "":
					reason = "initial"
				case unavailable:
					reason = "unreachable"
				case rotationDue:
					reason = "scheduled"
				default:
					reason = "better"
				}
			}
		}
		if chosen != sub.SelectedTag || sub.LastSwitch == 0 {
			updates["last_switch"] = automaticBalanceNow().Unix()
		}
		// A singleton/failed alternate should be retried next period, not every tick.
		if rotationDue && chosen == sub.SelectedTag {
			updates["last_switch"] = automaticBalanceNow().Unix()
		}
		message := probeWarning
		if chosen == "" {
			message = "No healthy outbound; automatic route is blocked"
		}
		updates["selected_tag"], updates["probe_error"], updates["switch_reason"] = chosen, message, reason
		result := database.GetDB().Model(&model.OutboundSubscription{}).
			Where("id = ? AND updated_at = ? AND last_fetched_outbounds = ? AND enabled = ? AND auto_balance = ?", sub.Id, sub.UpdatedAt, sub.LastFetchedOutbounds, true, true).
			Where("balance_mode = ? AND probe_url = ? AND switch_interval = ? AND health_interval = ? AND probe_interval = ? AND tolerance = ?", sub.BalanceMode, sub.ProbeURL, sub.SwitchInterval, sub.HealthInterval, sub.ProbeInterval, sub.Tolerance).Updates(updates)
		if result.Error != nil {
			return changes, result.Error
		}
		if result.RowsAffected > 0 && (chosen != sub.SelectedTag || sub.LastProbe == 0 && fullScan) {
			changes++
			logger.Infof("Outbound subscription %d auto selected %q (%s)", sub.Id, chosen, reason)
		}
	}
	return changes, nil
}
