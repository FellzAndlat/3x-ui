package service

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/util/common"
	"github.com/SawaMEN/3x-ui/v3/internal/util/link"
	"github.com/SawaMEN/3x-ui/v3/internal/util/netsafe"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
	"gorm.io/gorm"
)

// filterOutboundsRejectedByCore drops outbounds the vendored xray-core config
// loader refuses to build — since v26.7.11 that includes unencrypted
// vless/trojan outbounds to public addresses — because one such outbound in
// the merged config would keep the whole core from starting. When the running
// core predates that rejection, unencrypted outbounds are kept, mirroring
// CheckXrayConfig's version gate.
func filterOutboundsRejectedByCore(label string, outbounds []any) ([]any, []string) {
	coreVersion := "Unknown"
	if process := currentXrayProcess(); process != nil {
		coreVersion = process.GetXrayVersion()
	}
	kept := make([]any, 0, len(outbounds))
	var dropped []string
	for _, ob := range outbounds {
		if row, ok := ob.(map[string]any); ok && externalvpn.IsAdditionalOutbound(row) {
			if err := externalvpn.ValidateAdditionalOutbound(row); err != nil {
				dropped = append(dropped, err.Error())
			} else {
				kept = append(kept, ob)
			}
			continue
		}
		raw, err := json.Marshal(ob)
		if err == nil {
			if buildErr := xray.ValidateOutboundConfig(raw); buildErr != nil && !shouldSkipLegacyUnencryptedOutboundRejection(coreVersion, buildErr) {
				tag := ""
				if m, ok := ob.(map[string]any); ok {
					tag, _ = m["tag"].(string)
				}
				logger.Warningf("%s: incompatible xray outbound %q: %v", label, tag, buildErr)
				dropped = append(dropped, fmt.Sprintf("%s: %v", tag, buildErr))
				continue
			}
		}
		kept = append(kept, ob)
	}
	return kept, dropped
}

// maxOutboundSubscriptionBytes caps a single outbound subscription response.
// It is larger than the 2 MiB user-facing subscription cap because an outbound
// subscription may aggregate many upstream outbounds into one document.
const maxOutboundSubscriptionBytes int64 = 8 << 20

const defaultOutboundSubscriptionUserAgent = "3x-ui-outbound-sub/1.0"

var errOutboundSubscriptionBodyTooLarge = errors.New("outbound subscription response body exceeds size limit")

func readBoundedOutboundSubscriptionBody(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxOutboundSubscriptionBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxOutboundSubscriptionBytes {
		return nil, fmt.Errorf("%w (limit: %d bytes)", errOutboundSubscriptionBodyTooLarge, maxOutboundSubscriptionBytes)
	}
	return body, nil
}

// OutboundSubscriptionService manages remote outbound subscriptions.
var subscriptionOrderMu sync.Mutex
var subscriptionLocks = struct {
	sync.Mutex
	entries map[int]*subscriptionLock
}{entries: make(map[int]*subscriptionLock)}

type subscriptionLock struct {
	sync.Mutex
	users int
}

func lockSubscription(id int) func() {
	subscriptionLocks.Lock()
	entry := subscriptionLocks.entries[id]
	if entry == nil {
		entry = &subscriptionLock{}
		subscriptionLocks.entries[id] = entry
	}
	entry.users++
	subscriptionLocks.Unlock()
	entry.Lock()
	return func() {
		entry.Unlock()
		subscriptionLocks.Lock()
		entry.users--
		if entry.users == 0 {
			delete(subscriptionLocks.entries, id)
		}
		subscriptionLocks.Unlock()
	}
}

type OutboundSubscriptionService struct {
	settingService SettingService
}

// NewOutboundSubscriptionService returns a service for managing outbound subscriptions.
func NewOutboundSubscriptionService() *OutboundSubscriptionService {
	return &OutboundSubscriptionService{}
}

// List returns all subscriptions (newest first).
func (s *OutboundSubscriptionService) List() ([]*model.OutboundSubscription, error) {
	db := database.GetDB()
	var subs []*model.OutboundSubscription
	if err := db.Model(&model.OutboundSubscription{}).Order("priority asc, id asc").Find(&subs).Error; err != nil {
		return nil, err
	}
	for _, sub := range subs {
		sub.OutboundCount = countOutbounds(sub.LastFetchedOutbounds)
		// Don't ship the heavy raw blobs to the list view.
		sub.LastFetchedOutbounds = ""
		sub.LinkIdentities = ""
	}
	return subs, nil
}

// countOutbounds returns the number of outbounds in a stored LastFetchedOutbounds
// JSON array (0 for empty/invalid).
func countOutbounds(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	var arr []any
	if json.Unmarshal([]byte(raw), &arr) != nil {
		return 0
	}
	return len(arr)
}

// Get returns a single subscription by id.
func (s *OutboundSubscriptionService) Get(id int) (*model.OutboundSubscription, error) {
	db := database.GetDB()
	var sub model.OutboundSubscription
	if err := db.First(&sub, id).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

// Create persists a new subscription. It does not fetch immediately; the caller
// can call Refresh on the returned id if desired.
var defaultPrefixRe = regexp.MustCompile(`^sub(\d+)-$`)

// defaultPrefixNumber returns the smallest positive integer N that is not already
// in use as a "subN-" tag prefix among the given subscriptions. This is used to
// auto-name a subscription's outbounds when the user leaves the prefix blank, so
// deleting a subscription frees its number for reuse instead of letting the
// number grow forever with the auto-increment DB id. A subscription with a blank
// prefix reserves its own id (it falls back to id-based "sub<id>-" tags).
func defaultPrefixNumber(subs []*model.OutboundSubscription, excludeId int) int {
	used := map[int]bool{}
	for _, sub := range subs {
		if sub.Id == excludeId {
			continue
		}
		if sub.TagPrefix == "" {
			used[sub.Id] = true
			continue
		}
		if m := defaultPrefixRe.FindStringSubmatch(sub.TagPrefix); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				used[n] = true
			}
		}
	}
	n := 1
	for used[n] {
		n++
	}
	return n
}

// nextDefaultSubPrefix builds the default "subN-" prefix for a new/edited
// subscription, picking the smallest free N (excludeId skips a subscription's
// own current prefix when editing).
func (s *OutboundSubscriptionService) nextDefaultSubPrefix(excludeId int) (string, error) {
	var subs []*model.OutboundSubscription
	if err := database.GetDB().Find(&subs).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("sub%d-", defaultPrefixNumber(subs, excludeId)), nil
}

func (s *OutboundSubscriptionService) Create(remark, rawURL, tagPrefix, userAgent string, enabled bool, updateInterval int, allowPrivate, prepend, allowInsecure bool, balance ...*BalanceOptions) (*model.OutboundSubscription, error) {
	subscriptionOrderMu.Lock()
	defer subscriptionOrderMu.Unlock()
	cleanURL, err := SanitizePublicHTTPURL(rawURL, allowPrivate)
	if err != nil {
		return nil, common.NewError("invalid subscription URL:", err)
	}
	if cleanURL == "" {
		return nil, common.NewError("subscription URL is required")
	}
	if updateInterval <= 0 {
		updateInterval = 600
	}
	prefix := strings.TrimSpace(tagPrefix)
	if prefix == "" {
		prefix, err = s.nextDefaultSubPrefix(0)
		if err != nil {
			return nil, err
		}
	}
	// New subscriptions go to the end of the priority order.
	var lastPriority int
	if err := database.GetDB().Model(&model.OutboundSubscription{}).Select("COALESCE(MAX(priority), -1)").Scan(&lastPriority).Error; err != nil {
		return nil, err
	}
	sub := &model.OutboundSubscription{
		Remark:         strings.TrimSpace(remark),
		Url:            cleanURL,
		Enabled:        enabled,
		AllowPrivate:   allowPrivate,
		AllowInsecure:  allowInsecure,
		UserAgent:      strings.TrimSpace(userAgent),
		Prepend:        prepend,
		Priority:       lastPriority + 1,
		TagPrefix:      prefix,
		UpdateInterval: updateInterval,
	}
	if len(balance) == 0 || balance[0] == nil {
		balance = []*BalanceOptions{{Enabled: true, Mode: "latency", Interval: 300, Tolerance: 50, SwitchInterval: 900, HealthInterval: 30}}
		sub.Prepend = true
	}
	if len(balance) > 0 && balance[0] != nil {
		if err := balance[0].Validate(allowPrivate); err != nil {
			return nil, err
		}
		balance[0].apply(sub)
	}
	if err := database.GetDB().Create(sub).Error; err != nil {
		return nil, err
	}
	return sub, nil
}

// Update updates editable fields.
func (s *OutboundSubscriptionService) Update(id int, remark, rawURL, tagPrefix, userAgent string, enabled bool, updateInterval int, allowPrivate, prepend, allowInsecure bool, balance ...*BalanceOptions) error {
	unlock := lockSubscription(id)
	defer unlock()
	sub, err := s.Get(id)
	if err != nil {
		return err
	}
	cleanURL, err := SanitizePublicHTTPURL(rawURL, allowPrivate)
	if err != nil {
		return common.NewError("invalid subscription URL:", err)
	}
	if cleanURL == "" {
		return common.NewError("subscription URL is required")
	}
	if updateInterval <= 0 {
		updateInterval = 600
	}
	prefix := strings.TrimSpace(tagPrefix)
	if prefix == "" {
		prefix, err = s.nextDefaultSubPrefix(sub.Id)
		if err != nil {
			return err
		}
	}
	resetSelection := len(balance) > 0 && balance[0] != nil && (sub.AutoBalance != balance[0].Enabled || sub.BalanceMode != balance[0].Mode || sub.ProbeURL != balance[0].URL)
	sub.Remark = strings.TrimSpace(remark)
	sub.Url = cleanURL
	sub.Enabled = enabled
	sub.AllowPrivate = allowPrivate
	sub.AllowInsecure = allowInsecure
	sub.UserAgent = strings.TrimSpace(userAgent)
	sub.Prepend = prepend
	sub.TagPrefix = prefix
	sub.UpdateInterval = updateInterval
	if len(balance) > 0 && balance[0] != nil {
		if err := balance[0].Validate(allowPrivate); err != nil {
			return err
		}
		balance[0].apply(sub)
	}
	fields := []string{"remark", "url", "enabled", "allow_private", "allow_insecure", "user_agent", "prepend", "tag_prefix", "update_interval"}
	if len(balance) > 0 && balance[0] != nil {
		fields = append(fields, "auto_balance", "balance_mode", "probe_url", "probe_interval", "tolerance", "switch_interval", "health_interval", "last_health", "last_probe", "probe_error", "probe_results")
		// Include runtime resets only when the balance policy actually changed.
		if resetSelection {
			fields = append(fields, "selected_tag", "last_switch", "switch_reason", "applied_tag", "apply_error")
		}
	}
	return database.GetDB().Model(&model.OutboundSubscription{}).Where("id = ?", id).Select(fields).Updates(sub).Error
}

// Delete removes a subscription.
func (s *OutboundSubscriptionService) Delete(id int) error {
	unlock := lockSubscription(id)
	defer unlock()
	subscriptionOrderMu.Lock()
	defer subscriptionOrderMu.Unlock()
	return database.GetDB().Delete(&model.OutboundSubscription{}, id).Error
}

// GetLastOutbounds returns the last successfully fetched outbounds for a subscription
// (as raw interface slice ready for JSON merge). Returns nil slice when none.
func (s *OutboundSubscriptionService) GetLastOutbounds(id int) ([]any, error) {
	sub, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sub.LastFetchedOutbounds) == "" {
		return nil, nil
	}
	var arr []any
	if err := json.Unmarshal([]byte(sub.LastFetchedOutbounds), &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// Refresh fetches the subscription URL, parses the links, assigns stable tags,
// persists the results, and returns the runtime-compatible outbounds.
func (s *OutboundSubscriptionService) Refresh(id int) ([]any, error) {
	sub, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	outbounds, err := s.fetchAndStore(sub)
	return outbounds, err
}

// RefreshAllEnabled fetches every enabled subscription whose due time has passed
// (lastUpdated + updateInterval <= now). It returns the number of subscriptions
// whose stored outbound configuration changed.
func (s *OutboundSubscriptionService) RefreshAllEnabled() (int, error) {
	db := database.GetDB()
	var subs []*model.OutboundSubscription
	if err := db.Where("enabled = ?", true).Find(&subs).Error; err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	refreshed := 0
	for _, sub := range subs {
		due := sub.LastUpdated + int64(sub.UpdateInterval)
		if sub.LastUpdated == 0 || due <= now {
			if _, changed, err := s.fetchAndStoreWithChange(sub); err != nil {
				logger.Warningf("outbound sub %d (%s) refresh failed: %v", sub.Id, sub.Remark, err)
				// continue with others
			} else if changed {
				refreshed++
			}
		}
	}
	return refreshed, nil
}

// subscriptionFetchClient builds the HTTP client used to fetch a subscription.
// A configured panel egress proxy dials the loopback SOCKS bridge (xray handles
// the real egress), so its localhost dial must not be SSRF-blocked. A direct
// fetch dials the target itself and re-resolves the hostname at dial time, so it
// goes through the SSRF-guarded dialer, which resolves, checks and dials the same
// IP atomically — closing the DNS-rebinding gap left by validating the hostname
// separately from the dial.
func (s *OutboundSubscriptionService) subscriptionFetchClient(timeout time.Duration, allowInsecure bool) *http.Client {
	var client *http.Client
	if s.settingService.PanelEgressProxyURL() != "" {
		client = s.settingService.NewProxiedHTTPClient(timeout)
	} else {
		client = &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{DialContext: netsafe.SSRFGuardedDialContext},
		}
	}
	if allowInsecure {
		if tr, ok := client.Transport.(*http.Transport); ok && tr != nil {
			cloned := tr.Clone()
			if cloned.TLSClientConfig == nil {
				cloned.TLSClientConfig = &tls.Config{}
			}
			cloned.TLSClientConfig.InsecureSkipVerify = true
			client.Transport = cloned
		}
	}
	return client
}

// fetchAndStore does the actual network + parse + stability + persist work.
func (s *OutboundSubscriptionService) fetchAndStore(sub *model.OutboundSubscription) ([]any, error) {
	out, _, err := s.fetchAndStoreWithChange(sub)
	return out, err
}
func (s *OutboundSubscriptionService) fetchAndStoreWithChange(sub *model.OutboundSubscription) ([]any, bool, error) {
	unlock := lockSubscription(sub.Id)
	defer unlock()
	fresh, err := s.Get(sub.Id)
	if err != nil {
		return nil, false, err
	}
	*sub = *fresh
	previous := sub.LastFetchedOutbounds
	out, err := s.fetchAndStoreLocked(sub)
	return out, err == nil && previous != sub.LastFetchedOutbounds, err
}
func (s *OutboundSubscriptionService) fetchAndStoreLocked(sub *model.OutboundSubscription) ([]any, error) {
	// Re-sanitize on every fetch (handles legacy rows + defense in depth against
	// any direct DB tampering). Private targets are blocked unless this
	// subscription was explicitly created with AllowPrivate.
	cleanURL, err := SanitizePublicHTTPURL(sub.Url, sub.AllowPrivate)
	if err != nil {
		s.recordError(sub, err)
		return nil, err
	}
	if cleanURL == "" {
		return nil, common.NewError("subscription has no valid URL")
	}
	sub.Url = cleanURL // persist the cleaned version

	client := s.subscriptionFetchClient(30*time.Second, sub.AllowInsecure)
	// Re-validate every redirect hop: the initial host is checked above, but a
	// redirect could still point at a private/internal address (SSRF). Cap the
	// redirect chain as well.
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if sub.AllowPrivate {
			return nil
		}
		ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
		defer cancel()
		return rejectPrivateHost(ctx, req.URL.Hostname())
	}

	reqCtx := netsafe.ContextWithAllowPrivate(context.Background(), sub.AllowPrivate)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, sub.Url, nil)
	if err != nil {
		s.recordError(sub, err)
		return nil, err
	}
	userAgent := strings.TrimSpace(sub.UserAgent)
	if userAgent == "" {
		userAgent = defaultOutboundSubscriptionUserAgent
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		s.recordError(sub, err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("http %d", resp.StatusCode)
		s.recordError(sub, err)
		return nil, err
	}
	body, err := readBoundedOutboundSubscriptionBody(resp.Body)
	if err != nil {
		s.recordError(sub, err)
		return nil, err
	}

	parsed, identities, err := link.ParseSubscriptionBody(body)
	if err != nil {
		s.recordError(sub, err)
		return nil, err
	}

	// Load previous identities -> tags for stability.
	prev := map[string]string{}
	if strings.TrimSpace(sub.LinkIdentities) != "" {
		_ = json.Unmarshal([]byte(sub.LinkIdentities), &prev)
	}

	// Also load previous outbounds so we can reuse tags even for identities we
	// temporarily lost (defensive). All parsed profiles are persisted regardless
	// of the currently selected core, so positional stability is core-independent.
	prevTagByIndex := map[int]string{}
	if strings.TrimSpace(sub.LastFetchedOutbounds) != "" {
		var prevObs []any
		if json.Unmarshal([]byte(sub.LastFetchedOutbounds), &prevObs) == nil {
			for i, o := range prevObs {
				if m, ok := o.(map[string]any); ok {
					if tag, _ := m["tag"].(string); tag != "" {
						prevTagByIndex[i] = tag
					}
				}
			}
		}
	}

	// Assign tags to every parsed profile before compatibility filtering. A
	// profile unsupported by the active core stays stored with a stable identity
	// and can become usable after switching cores or upgrading the core.
	assigned := assignStableTags(parsed, identities, prev, prevTagByIndex, sub.Id, sub.TagPrefix)

	// Persist identities for next time.
	newIdent := map[string]string{}
	for i, id := range identities {
		if i < len(assigned) {
			newIdent[id] = assigned[i]
		}
	}
	identJSON, _ := json.Marshal(newIdent)

	stored := make([]any, len(parsed))
	for i := range parsed {
		stored[i] = map[string]any(parsed[i])
	}

	// Compatibility is a runtime property, not a storage filter. Validate all
	// members against the selected core to produce an explicit user-facing
	// status. The same compatible slice is returned by Refresh for backwards
	// compatibility; the full source set remains persisted for future core
	// switches/upgrades.
	runtimeCompatible, compatibilityIssues, coreName := filterSubscriptionOutboundsWithIssues(
		fmt.Sprintf("outbound sub %d", sub.Id), stored,
	)

	// Persist the complete outbound set (as compact JSON array).
	obsJSON, _ := json.Marshal(stored)

	changed := sub.LastFetchedOutbounds != string(obsJSON)
	if changed {
		sub.LastProbe = 0
	}
	sub.LastFetchedOutbounds = string(obsJSON)
	sub.LinkIdentities = string(identJSON)
	sub.LastUpdated = time.Now().Unix()
	sub.LastError = ""
	if len(compatibilityIssues) > 0 {
		sub.LastError = fmt.Sprintf(
			"%d outbound(s) unsupported by current %s core; kept in subscription but excluded from runtime: %s",
			len(compatibilityIssues), coreName, strings.Join(compatibilityIssues, "; "),
		)
	}

	updates := map[string]any{
		"last_fetched_outbounds": sub.LastFetchedOutbounds, "link_identities": sub.LinkIdentities,
		"last_updated": sub.LastUpdated, "last_error": sub.LastError,
	}
	if changed {
		updates["last_probe"] = 0
	}
	// A fetch must not overwrite automatic selection or concurrently edited settings.
	result := database.GetDB().Model(&model.OutboundSubscription{}).
		Where("id = ? AND url = ? AND tag_prefix = ?", sub.Id, sub.Url, sub.TagPrefix).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("subscription changed during refresh; retry")
	}
	return runtimeCompatible, nil
}

func (s *OutboundSubscriptionService) recordError(sub *model.OutboundSubscription, err error) {
	sub.LastError = err.Error()
	_ = database.GetDB().Model(sub).Update("last_error", sub.LastError).Error
}

// assignStableTags assigns a tag to each parsed outbound, preferring stability:
//  1. reuse the tag previously mapped to the link's identity (prev),
//  2. else reuse the tag at the same position from the last fetch (prevTagByIndex),
//  3. else allocate a fresh tag from the prefix + remark (link.SuggestTag).
//
// Tags are kept unique within the batch by appending "-N" on collision, and are
// written back into parsed[i]["tag"]. The returned slice holds the assigned tags
// in order. When tagPrefix is empty a "sub<subID>-" prefix is used for fresh tags.
func assignStableTags(parsed []link.Outbound, identities []string, prev map[string]string, prevTagByIndex map[int]string, subID int, tagPrefix string) []string {
	reservedStableTags := map[string]bool{}
	for i := range parsed {
		if i < len(identities) && prev[identities[i]] != "" {
			reservedStableTags[prev[identities[i]]] = true
		}
	}

	used := map[string]bool{} // uniqueness within this refresh batch
	assigned := make([]string, len(parsed))
	for i := range parsed {
		id := ""
		if i < len(identities) {
			id = identities[i]
		}
		candidate := ""
		identityTag := ""
		if old, ok := prev[id]; ok && old != "" {
			candidate = old
			identityTag = old
		}
		if candidate == "" {
			// try to reuse by rough positional match from previous fetch (best effort)
			if old, ok := prevTagByIndex[i]; ok && old != "" && !reservedStableTags[old] {
				candidate = old
			}
		}
		if candidate == "" {
			// fresh allocation
			prefix := tagPrefix
			if prefix == "" {
				prefix = fmt.Sprintf("sub%d-", subID)
			}
			remark := ""
			if m, ok := parsed[i]["tag"].(string); ok {
				remark = m
			}
			candidate = link.SuggestTag(prefix, remark, i)
		}
		// ensure local uniqueness inside this batch
		final := candidate
		for k := 1; used[final] || (reservedStableTags[final] && final != identityTag); k++ {
			final = fmt.Sprintf("%s-%d", candidate, k)
		}
		used[final] = true
		assigned[i] = final

		// write back the tag into the outbound
		parsed[i]["tag"] = final
	}
	return assigned
}

// AllActiveOutbounds returns the concatenation of the last-fetched outbounds
// for every enabled subscription. This is the set that should be merged into
// the final Xray config. Order: subscription creation order (by id asc) so
// that later subscriptions can shadow earlier ones if the admin uses colliding
// prefixes (last writer wins inside xray, but we try to keep tags unique).
func (s *OutboundSubscriptionService) AllActiveOutbounds() ([]any, error) {
	prepend, appendList, err := s.activeOutboundsSplit()
	if err != nil {
		return nil, err
	}
	return append(prepend, appendList...), nil
}

// activeOutboundsSplit returns the active subscription outbounds split into those
// that should be placed BEFORE the manual template outbounds (Prepend) and those
// placed AFTER. Within each group, subscriptions are ordered by Priority (then id)
// so the admin can control the merged order.
func (s *OutboundSubscriptionService) activeOutboundsSplit() (prepend []any, appendList []any, err error) {
	db := database.GetDB()
	var subs []*model.OutboundSubscription
	if err := db.Where("enabled = ?", true).Order("priority asc, id asc").Find(&subs).Error; err != nil {
		return nil, nil, err
	}
	for _, sub := range subs {
		raw := sub.LastFetchedOutbounds
		if strings.TrimSpace(raw) == "" {
			if !sub.AutoBalance {
				continue
			}
			raw = "[]"
		}
		var arr []any
		if err := json.Unmarshal([]byte(raw), &arr); err != nil {
			logger.Warningf("outbound sub %d has corrupt LastFetchedOutbounds: %v", sub.Id, err)
			continue
		}
		arr = filterSubscriptionOutbounds(fmt.Sprintf("outbound sub %d", sub.Id), arr)
		arr = subscriptionAutoOutbound(sub, arr)
		if sub.Prepend {
			prepend = append(prepend, arr...)
		} else {
			appendList = append(appendList, arr...)
		}
	}
	return prepend, appendList, nil
}

// Move shifts a subscription one step up or down in the priority order and
// re-normalizes all priorities to a 0..n-1 sequence.
func (s *OutboundSubscriptionService) Move(id int, up bool) error {
	subscriptionOrderMu.Lock()
	defer subscriptionOrderMu.Unlock()
	return database.GetDB().Transaction(func(db *gorm.DB) error {
		var subs []*model.OutboundSubscription
		if err := db.Order("priority asc, id asc").Find(&subs).Error; err != nil {
			return err
		}
		idx := -1
		for i, sub := range subs {
			if sub.Id == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return common.NewError("subscription not found")
		}
		swap := idx + 1
		if up {
			swap = idx - 1
		}
		if swap < 0 || swap >= len(subs) {
			return nil // already at the edge
		}
		subs[idx], subs[swap] = subs[swap], subs[idx]
		for i, sub := range subs {
			if sub.Priority != i {
				if err := db.Model(sub).Update("priority", i).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// AllActiveOutboundTags returns only the tags of active subscription outbounds.
// Useful for populating balancer / routing selectors without shipping full objects.
func (s *OutboundSubscriptionService) AllActiveOutboundTags() ([]string, error) {
	obs, err := s.AllActiveOutbounds()
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(obs))
	for _, o := range obs {
		if m, ok := o.(map[string]any); ok {
			if t, _ := m["tag"].(string); t != "" {
				tags = append(tags, t)
			}
		}
	}
	return tags, nil
}

/*
Tag stability strategy (important for balancers and routing rules)

When a subscription is refreshed we try very hard to keep the *same* tag for the
same logical outbound so that existing balancers and routing rules keep working.

How we do it:
- On every successful parse we compute a stable "identity" for each link
  (the core of the URI with the remark fragment removed, or for vmess the inner
  JSON without the "ps" field).
- We persist a map identity -> tag in the LinkIdentities column.
- On the next refresh, if we see the same identity again we reuse the previous tag,
  even if the remark changed or minor parameters moved.
- Only when we have never seen the identity before do we allocate a fresh tag
  using the user-supplied TagPrefix + slug(remark) (or an index fallback).
- Within one refresh we still deduplicate with -N suffixes.

Consequences for balancers / routing:
- If you use an *exact* tag in a balancer selector or a routing rule, that
  specific server will continue to be used after refreshes (as long as the
  provider still returns a link that produces the same identity).
- If you use a *prefix/wildcard* selector (e.g. "hk-*", "sg-.*"), then any
  *new* servers that the subscription later returns will automatically be
  eligible for that balancer on the next Xray reload — this is the recommended
  way to "subscribe to a pool".
- When a server disappears from the subscription, its tag simply stops
  existing in the final outbounds array. The balancer will have fewer
  candidates. If you configured a `fallbackTag` on the balancer, Xray will use
  it. Otherwise connections that would have used the missing member may fail
  or be routed by the next rule.
- If the provider rotates credentials/UUIDs/hosts for a server, the identity
  changes → we treat it as a brand new outbound and give it a new tag. Any
  balancer/rule that referenced the *old* tag will no longer see it. This is
  an inherent limitation of subscription-based outbounds.

We deliberately do *not* mutate the saved xrayTemplateConfig. Subscription
outbounds are always injected at runtime in GetXrayConfig.
*/

func (s *OutboundSubscriptionService) activeAutomaticSubscriptions() ([]*model.OutboundSubscription, error) {
	var subs []*model.OutboundSubscription
	err := database.GetDB().Where("enabled = ? AND auto_balance = ?", true, true).Order("priority asc, id asc").Find(&subs).Error
	return subs, err
}
