package sub

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/snell"
	"github.com/SawaMEN/3x-ui/v3/internal/tuic"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/util/random"
	wgutil "github.com/SawaMEN/3x-ui/v3/internal/util/wireguard"
)

//go:embed default.json
var defaultJson string

// SubJsonService handles JSON subscription configuration generation and management.
type SubJsonService struct {
	configJson       map[string]any
	defaultOutbounds []json_util.RawMessage
	finalMask        string
	mux              string
	observatory      subBalancerObservatoryConfig

	// bakedRouting is re-resolved per request: a remote URL may be cold at
	// construction time and warm up later via the cron job.
	routingRules   string
	bakedRoutingMu sync.Mutex
	bakedRouting   *bakedRoutingState

	// dnsBlock is the panel DNS override, fixed for the service's lifetime.
	dnsBlock map[string]any

	SubService *SubService
}

type bakedRoutingState struct {
	spec       jsonRoutingSpec
	configJson map[string]any
}

// NewSubJsonService creates a new JSON subscription service with the given configuration.
func NewSubJsonService(mux string, rules string, finalMask string, routingRules string, subService *SubService) *SubJsonService {
	var configJson map[string]any
	var defaultOutbounds []json_util.RawMessage
	_ = json.Unmarshal([]byte(defaultJson), &configJson)
	if outboundSlices, ok := configJson["outbounds"].([]any); ok {
		for _, defaultOutbound := range outboundSlices {
			jsonBytes, _ := json.Marshal(defaultOutbound)
			defaultOutbounds = append(defaultOutbounds, jsonBytes)
		}
	}

	// A baked routing profile replaces the template's dns and routing subtrees
	// outright; the legacy simple-rules setting only applies without a profile.
	if routingRules == "" && rules != "" {
		var newRules []any
		routing, _ := configJson["routing"].(map[string]any)
		defaultRules, _ := routing["rules"].([]any)
		_ = json.Unmarshal([]byte(rules), &newRules)
		defaultRules = append(newRules, defaultRules...)
		routing["rules"] = defaultRules
		configJson["routing"] = routing
	}

	return &SubJsonService{
		configJson:       configJson,
		defaultOutbounds: defaultOutbounds,
		finalMask:        finalMask,
		mux:              mux,
		routingRules:     routingRules,
		observatory:      defaultSubBalancerObservatoryConfig(),
		SubService:       subService,
	}
}

// Re-resolved per call so an upstream edit reaches the documents without a
// restart; a failed resolve keeps the last good template.
func (s *SubJsonService) bakedTemplate() map[string]any {
	if s.routingRules == "" && s.dnsBlock == nil {
		return s.configJson
	}
	spec := resolveJsonRoutingSpec(s.routingRules)
	s.bakedRoutingMu.Lock()
	defer s.bakedRoutingMu.Unlock()
	if s.bakedRouting != nil {
		if spec.empty() || spec.equal(s.bakedRouting.spec) {
			return s.bakedRouting.configJson
		}
	} else if spec.empty() && s.dnsBlock == nil {
		return s.configJson
	}
	template := make(map[string]any, len(s.configJson)+2)
	maps.Copy(template, s.configJson)
	if !spec.empty() {
		applyJsonRouting(template, spec)
	}
	// The panel-level DNS block is an explicit choice, so it also replaces the
	// dns subtree a routing profile would otherwise bake in.
	if s.dnsBlock != nil {
		template["dns"] = s.dnsBlock
	}
	s.bakedRouting = &bakedRoutingState{spec: spec, configJson: template}
	return template
}

// GetJson generates a JSON subscription configuration for the given subscription ID and host.
func (s *SubJsonService) GetJson(subId string, host string, alwaysReturnArray bool) (string, string, error) {
	subReq := s.SubService.ForRequest(host)
	subReq.subscriptionBody = true
	inbounds, err := subReq.getInboundsBySubId(subId)
	if err != nil {
		return "", "", err
	}
	externalLinks, err := subReq.getClientExternalLinksBySubId(subId)
	if err != nil {
		return "", "", err
	}
	if len(inbounds) == 0 && len(externalLinks) == 0 {
		return "", "", nil
	}
	// JSON uses Xray's outbound model. Refuse the whole format when any linked
	// inbound cannot be represented, otherwise auto-detection could silently
	// drop one connection and still return HTTP 200.
	if containsUnsupportedJSONProtocol(inbounds) || containsShadowTLSTransport(inbounds) {
		return "", "", errSubscriptionFormatUnsupported
	}

	var header string
	var hasInactiveExternal bool
	var hasEnabledClient bool

	seenEmails := make(map[string]struct{})
	entries := make([]subConfigEntry, 0, len(inbounds))
	// Prepare Inbounds
	for _, inbound := range inbounds {
		clients := subReq.matchingClients(inbound, subId)
		if len(clients) == 0 {
			continue
		}
		subReq.projectThroughFallbackMaster(inbound)
		hostEps := subReq.hostEndpoints(inbound, "json")
		if hostEps != nil && len(hostEps) == 0 {
			for _, client := range clients {
				seenEmails[client.Email] = struct{}{}
				hasEnabledClient = hasEnabledClient || client.Enable
			}
			continue
		}
		if len(hostEps) > 0 {
			injectExternalProxy(inbound, hostEps)
			delete(subReq.streamSettingsByInbound, inbound.Id)
		}

		var inboundConfigs []json_util.RawMessage
		for _, client := range clients {
			if client.Enable {
				hasEnabledClient = true
			}
			seenEmails[client.Email] = struct{}{}
			if !client.Enable {
				continue
			}
			configs := s.getConfig(subReq, inbound, client, host)
			if len(configs) == 0 {
				return "", "", errSubscriptionFormatUnsupported
			}
			inboundConfigs = append(inboundConfigs, configs...)
		}
		if len(inboundConfigs) > 0 {
			entries = append(entries, subConfigEntry{
				sortIndex: inbound.SubSortIndex,
				id:        inbound.Id,
				configs:   inboundConfigs,
			})
		}
	}
	entries = s.appendBalancerEntries(entries)

	// Inbounds arrive sorted by (sub_sort_index, id); balancers interleave by
	// the same key and, on an equal number, follow the inbound group.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].sortIndex != entries[j].sortIndex {
			return entries[i].sortIndex < entries[j].sortIndex
		}
		if entries[i].kind != entries[j].kind {
			return entries[i].kind < entries[j].kind
		}
		return entries[i].id < entries[j].id
	})
	var configArray []json_util.RawMessage
	for _, entry := range entries {
		configArray = append(configArray, entry.configs...)
	}
	for _, ext := range externalLinks {
		if ext.Enable {
			hasEnabledClient = true
		}
		if !ext.Active {
			seenEmails[ext.Email] = struct{}{}
			hasInactiveExternal = true
			continue
		}
		for _, el := range expandEntry(ext) {
			outbound := parsedExternalOutbound(el.Link)
			if outbound == nil {
				return "", "", errSubscriptionFormatUnsupported
			}
			seenEmails[ext.Email] = struct{}{}
			remark := el.Name
			if remark == "" {
				remark = ext.Email
			}
			newOutbounds := []json_util.RawMessage{outbound}
			newOutbounds = append(newOutbounds, s.defaultOutbounds...)
			newConfigJson := make(map[string]any)
			maps.Copy(newConfigJson, s.bakedTemplate())
			newConfigJson["outbounds"] = newOutbounds
			newConfigJson["remarks"] = remark
			newConfig, _ := json.MarshalIndent(newConfigJson, "", "  ")
			configArray = append(configArray, newConfig)
		}
	}

	if len(configArray) == 0 && !hasInactiveExternal {
		return "", "", nil
	}

	emails := make([]string, 0, len(seenEmails))
	for e := range seenEmails {
		emails = append(emails, e)
	}
	slices.Sort(emails)
	traffic, _ := subReq.AggregateTrafficByEmails(emails)
	traffic.Enable = hasEnabledClient
	header = subReq.subscriptionUserinfo(traffic)

	if mode, remark := subReq.resolveInfoNodeRemark(subId, emails, traffic, len(configArray) > 0); mode != infoNodeNone {
		dummyConfig := s.genDummySocksConfig(remark)
		if mode == infoNodeExpired || mode == infoNodeDepleted {
			configArray = []json_util.RawMessage{dummyConfig}
		} else {
			configArray = append([]json_util.RawMessage{dummyConfig}, configArray...)
		}
	}

	if len(configArray) == 0 {
		return "", header, nil
	}

	var finalJson []byte
	if len(configArray) == 1 && !alwaysReturnArray {
		finalJson, _ = json.MarshalIndent(configArray[0], "", "  ")
	} else {
		finalJson, _ = json.MarshalIndent(configArray, "", "  ")
	}

	return string(finalJson), header, nil
}

// GetSingBoxJson converts the panel's per-client JSON profiles to native sing-box configs.
// The existing /json/ format remains Xray-compatible; callers opt into this format
// explicitly with ?format=sing-box so existing subscriptions are not changed.
func (s *SubJsonService) GetSingBoxJson(subId string, host string, alwaysReturnArray bool) (string, string, error) {
	return s.getSingBoxJson(subId, host, alwaysReturnArray, false)
}

// Hiddify expects a single JSON document with an outbounds array, including
// the hidden ShadowTLS transport used by the visible inner proxy.
func (s *SubJsonService) GetHiddifySingBoxJson(subId string, host string) (string, string, error) {
	return s.getSingBoxJson(subId, host, false, true)
}

func (s *SubJsonService) getSingBoxJson(subId string, host string, alwaysReturnArray, hiddify bool) (string, string, error) {
	// Native subscriptions intentionally do not call GetJson(). We still reuse
	// the panel's mature per-client endpoint generation helpers internally, but
	// collect and translate the model-backed entries before an Xray document is
	// assembled. This keeps subscription filtering, node fallback, external
	// proxy handling and client eligibility identical to the panel's source of
	// truth without depending on serialized Xray subscription documents.
	subReq := s.SubService.ForRequest(host)
	subReq.subscriptionBody = true
	inbounds, err := subReq.getInboundsBySubId(subId)
	if err != nil {
		return "", "", err
	}
	externalLinks, err := subReq.getClientExternalLinksBySubId(subId)
	if err != nil {
		return "", "", err
	}
	if len(inbounds) == 0 && len(externalLinks) == 0 {
		return "", "", nil
	}

	type nativeOutbound struct {
		out map[string]any
	}
	var proxies []nativeOutbound
	seenEmails := make(map[string]struct{})
	hasEnabledClient := false
	hasInactiveExternal := false

	// Cache matched clients across the WireGuard pre-pass and the generic
	// renderer. Without this, multi-protocol subscriptions query the same
	// client_inbounds relation twice per inbound.
	matchedClients := make(map[*model.Inbound][]model.Client, len(inbounds))
	clientsFor := func(inbound *model.Inbound) []model.Client {
		if clients, ok := matchedClients[inbound]; ok {
			return clients
		}
		clients := subReq.matchingClients(inbound, subId)
		matchedClients[inbound] = clients
		return clients
	}
	var excluded []*model.Inbound
	inbounds, excluded = subReq.partitionHostFormat(inbounds, "json")
	for _, inbound := range excluded {
		for _, client := range clientsFor(inbound) {
			seenEmails[client.Email] = struct{}{}
			hasEnabledClient = hasEnabledClient || client.Enable
		}
	}
	// Disabled clients contribute metadata but must not make an unsupported
	// protocol veto the formats available to the remaining active clients.
	inbounds = slices.DeleteFunc(inbounds, func(inbound *model.Inbound) bool {
		active := false
		for _, client := range clientsFor(inbound) {
			seenEmails[client.Email] = struct{}{}
			active = active || client.Enable
		}
		return !active
	})
	if len(inbounds) == 0 && len(externalLinks) == 0 {
		emails := make([]string, 0, len(seenEmails))
		for email := range seenEmails {
			emails = append(emails, email)
		}
		traffic, _ := subReq.AggregateTrafficByEmails(emails)
		traffic.Enable = hasEnabledClient
		return "", subReq.subscriptionUserinfo(traffic), nil
	}

	// Detached users are represented by inactive metadata entries so their
	// subscriptions remain addressable. Those entries are not proxy links and
	// must not turn a WireGuard-only subscription into a mixed profile.
	wireguardOnly := len(inbounds) > 0
	for _, entry := range externalLinks {
		if entry.Active {
			wireguardOnly = false
			break
		}
	}
	var wireguardConfigs []json.RawMessage
	for _, inbound := range inbounds {
		clients := clientsFor(inbound)
		if inbound.Protocol != model.WireGuard {
			if len(clients) > 0 {
				wireguardOnly = false
			}
			continue
		}
		if len(clients) == 0 {
			continue
		}
		subReq.projectThroughFallbackMaster(inbound)
		var settings map[string]any
		if endpoints := subReq.hostEndpoints(inbound, "json"); len(endpoints) > 0 {
			injectExternalProxy(inbound, endpoints)
			delete(subReq.streamSettingsByInbound, inbound.Id)
		}
		_ = json.Unmarshal([]byte(inbound.Settings), &settings)
		secretKey, _ := settings["secretKey"].(string)
		if secretKey == "" {
			continue
		}
		serverPublicKey := ""
		if pub, err := wgutil.PublicKeyFromPrivate(secretKey); err == nil {
			serverPublicKey = pub
		}
		for _, client := range clients {
			seenEmails[client.Email] = struct{}{}
			if !client.Enable || client.PrivateKey == "" || serverPublicKey == "" {
				continue
			}
			addresses := append([]string(nil), client.AllowedIPs...)
			if len(addresses) == 0 {
				continue
			}
			for _, shareEndpoint := range subReq.shareEndpointsForInbound(inbound) {
				peerAddress := shareEndpoint.Address
				if peerAddress == "" {
					peerAddress = wireguardPeerAddress(inbound, subReq.resolveInboundAddress(inbound), nil)
				}
				peer := map[string]any{
					"address":     peerAddress,
					"port":        shareEndpoint.Port,
					"public_key":  serverPublicKey,
					"allowed_ips": []string{"0.0.0.0/0", "::/0"},
				}
				if client.PreSharedKey != "" {
					peer["pre_shared_key"] = client.PreSharedKey
				}
				if client.KeepAlive != nil && *client.KeepAlive > 0 {
					peer["persistent_keepalive_interval"] = *client.KeepAlive
				}
				endpoint := map[string]any{
					"type":        "wireguard",
					"tag":         "wg-endpoint",
					"address":     addresses,
					"private_key": client.PrivateKey,
					"peers":       []any{peer},
				}
				if mtu, ok := settings["mtu"].(float64); ok && mtu > 0 {
					endpoint["mtu"] = int(mtu)
				}

				cfg := map[string]any{
					"$schema":   "https://sing-box.sagernet.org/schema.json",
					"endpoints": []any{endpoint},
					"inbounds": []any{map[string]any{
						"type":         "tun",
						"tag":          "tun-in",
						"address":      addresses,
						"auto_route":   true,
						"strict_route": true,
					}},
					"outbounds": []any{
						map[string]any{"type": "direct", "tag": "direct"},
						map[string]any{"type": "block", "tag": "blocked"},
					},
					"route": map[string]any{
						"rules":                 []any{map[string]any{"action": "route", "outbound": "wg-endpoint"}},
						"final":                 "direct",
						"auto_detect_interface": true,
					},
				}
				encoded, err := json.MarshalIndent(cfg, "", "  ")
				if err == nil {
					wireguardConfigs = append(wireguardConfigs, encoded)
					hasEnabledClient = true
				}
			}
		}
	}

	if wireguardOnly {
		if len(wireguardConfigs) == 0 {
			return "", "", errSubscriptionFormatUnsupported
		}
		emails := make([]string, 0, len(seenEmails))
		for email := range seenEmails {
			emails = append(emails, email)
		}
		slices.Sort(emails)
		traffic, _ := subReq.AggregateTrafficByEmails(emails)
		traffic.Enable = hasEnabledClient
		header := subReq.subscriptionUserinfo(traffic)
		if len(wireguardConfigs) == 1 && !alwaysReturnArray {
			return string(wireguardConfigs[0]), header, nil
		}
		arr, err := json.MarshalIndent(wireguardConfigs, "", "  ")
		if err != nil {
			return "", header, err
		}
		return string(arr), header, nil
	}

	// Refuse partial sing-box output during ordinary auto-detection. For
	// Hiddify with ShadowTLS, retain importable nodes even when other protocols
	// cannot be represented; otherwise Hiddify loses the ShadowTLS node too.
	// WireGuard endpoint profiles are emitted separately above. A mixed
	// WireGuard + proxy subscription cannot be represented by the current
	// separated-profile builder without routing one profile through another,
	// so reject it instead of falling back to the deprecated WireGuard outbound.
	shadowTLSForHiddify := hiddify && (containsSubscriptionProtocol(inbounds, model.ShadowTLS) || containsShadowTLSTransport(inbounds))
	if !shadowTLSForHiddify && (containsSubscriptionProtocol(inbounds, model.WireGuard) || containsUnsupportedSingBoxProtocol(inbounds)) {
		return "", "", errSubscriptionFormatUnsupported
	}

	formatUnsupported := false
	shadowTLSGenerated := false
	for _, inbound := range inbounds {
		clients := clientsFor(inbound)
		if len(clients) == 0 {
			continue
		}
		// Hiddify can import the supported sing-box outbounds even when another
		// inbound in this subscription is not representable as an outbound.
		// Do not let that unrelated inbound remove ShadowTLS from the profile.
		if shadowTLSForHiddify && (inbound.Protocol == model.WireGuard ||
			singBoxUnsupportedProtocol(inbound.Protocol)) {
			continue
		}
		subReq.projectThroughFallbackMaster(inbound)
		hostEps := subReq.hostEndpoints(inbound, "json")
		if hostEps != nil && len(hostEps) == 0 {
			for _, client := range clients {
				seenEmails[client.Email] = struct{}{}
				hasEnabledClient = hasEnabledClient || client.Enable
			}
			continue
		}
		if len(hostEps) > 0 {
			injectExternalProxy(inbound, hostEps)
			delete(subReq.streamSettingsByInbound, inbound.Id)
		}
		for _, client := range clients {
			seenEmails[client.Email] = struct{}{}
			if client.Enable {
				hasEnabledClient = true
			}
			if !client.Enable {
				continue
			}
			if model.ShadowTLSTransport(inbound.Settings) != nil {
				generated := 0
				if inbound.Protocol == model.AnyTLS {
					for _, endpoint := range subReq.shareEndpointsForInbound(inbound) {
						if strings.EqualFold(strings.TrimSpace(endpoint.ForceTls), "none") {
							continue
						}
						native := s.genNativeTLSLikeEndpoint(subReq, inbound, client, endpoint)
						if s.wrapNativeShadowTLS(inbound, native, endpoint.ep) {
							generated++
							native["tag"] = fmt.Sprintf("ShadowTLS · %s-%d", client.Email, len(proxies)+1)
							proxies = append(proxies, nativeOutbound{out: native})
						}
					}
				} else {
					var endpointCopies []map[string]any
					for index, raw := range s.getConfig(subReq, inbound, client, host, &endpointCopies) {
						var profile map[string]any
						if json.Unmarshal(raw, &profile) != nil {
							continue
						}
						outs, _ := profile["outbounds"].([]any)
						if len(outs) == 0 {
							continue
						}
						xray, _ := outs[0].(map[string]any)
						if xray == nil {
							continue
						}
						// ShadowTLS replaces the outer Xray transport, including any
						// global FinalMask or TLS defaults in the JSON template.
						xray["streamSettings"] = map[string]any{"network": "tcp", "security": "none"}
						native, err := singbox.TranslateXrayOutbound(xray)
						if err != nil || !s.wrapNativeShadowTLS(inbound, native, endpointCopies[index]) {
							continue
						}
						generated++
						native["tag"] = fmt.Sprintf("ShadowTLS · %s-%d", client.Email, len(proxies)+1)
						proxies = append(proxies, nativeOutbound{out: native})
					}
				}
				if generated == 0 {
					formatUnsupported = true
				} else {
					shadowTLSGenerated = true
				}
				continue
			}
			if inbound.Protocol == model.TUIC {
				stream := unmarshalStreamSettings(inbound.StreamSettings)
				externalProxies, _ := stream["externalProxy"].([]any)
				if len(externalProxies) == 0 {
					externalProxies = []any{nil}
				}
				generated := 0
				for _, rawEndpoint := range externalProxies {
					var endpoint map[string]any
					if rawEndpoint != nil {
						endpoint, _ = rawEndpoint.(map[string]any)
						if endpoint == nil {
							continue
						}
						forceTLS, _ := endpoint["forceTls"].(string)
						if strings.EqualFold(strings.TrimSpace(forceTLS), "none") {
							continue
						}
					}

					clone := *inbound
					// Native sing-box clients must receive the advertised server
					// address, never the server-side wildcard/loopback bind.
					clone.Listen = subReq.resolveInboundAddress(inbound)
					if clone.Listen == "" {
						clone.Listen = inbound.Listen
					}
					newStream := cloneStreamForExternalProxy(stream)
					delete(newStream, "externalProxy")
					if endpoint != nil {
						if dest, ok := endpoint["dest"].(string); ok && strings.TrimSpace(dest) != "" {
							clone.Listen = strings.TrimSpace(dest)
						}
						if rawPort, ok := endpoint["port"].(float64); ok && int(rawPort) > 0 {
							clone.Port = int(rawPort)
						}
						security, _ := newStream["security"].(string)
						applyExternalProxyTLSToStream(endpoint, newStream, security)
						applyHostStreamOverrides(endpoint, newStream)
					}
					nativeRaw := s.genNativeTUIC(&clone, newStream, client)
					if nativeRaw == nil {
						continue
					}
					var native map[string]any
					if err := json.Unmarshal(nativeRaw, &native); err != nil || native == nil {
						continue
					}
					tag := client.Email
					if tag == "" {
						tag = fmt.Sprintf("proxy-%d", len(proxies)+1)
					}
					if len(proxies) > 0 {
						tag = fmt.Sprintf("%s-%d", tag, len(proxies)+1)
					}
					native["tag"] = tag
					proxies = append(proxies, nativeOutbound{out: native})
					generated++
				}
				if generated == 0 {
					formatUnsupported = true
				}
				continue
			}
			if inbound.Protocol == model.AnyTLS || inbound.Protocol == model.ShadowTLS {
				generated := 0
				for _, endpoint := range subReq.shareEndpointsForInbound(inbound) {
					if strings.EqualFold(strings.TrimSpace(endpoint.ForceTls), "none") {
						continue
					}
					native := s.genNativeTLSLikeEndpoint(subReq, inbound, client, endpoint)
					if native == nil {
						continue
					}
					tag := client.Email
					if inbound.Protocol == model.ShadowTLS {
						tag = "ShadowTLS · " + tag
					}
					if tag == "" {
						tag = fmt.Sprintf("proxy-%d", len(proxies)+1)
					}
					if len(proxies) > 0 {
						tag = fmt.Sprintf("%s-%d", tag, len(proxies)+1)
					}
					native["tag"] = tag
					proxies = append(proxies, nativeOutbound{out: native})
					generated++
				}
				if generated == 0 {
					formatUnsupported = true
				}
				if inbound.Protocol == model.ShadowTLS && generated > 0 {
					shadowTLSGenerated = true
				}
				continue
			}
			if inbound.Protocol == model.Snell {
				settings, err := snell.Parse(inbound.Settings)
				generated := 0
				if err == nil && client.Password != "" {
					for _, endpoint := range subReq.shareEndpointsForInbound(inbound) {
						native := settings.Outbound(endpoint.Address, endpoint.Port, client)
						native["tag"] = fmt.Sprintf("Snell · %s-%d", client.Email, len(proxies)+1)
						proxies = append(proxies, nativeOutbound{out: native})
						generated++
					}
				}
				if generated == 0 {
					formatUnsupported = true
				}
				continue
			}
			if inbound.Protocol == model.NaiveProxy {
				// Naive is not an Xray outbound, but sing-box has a native
				// representation. Keep it in the structured profile instead of
				// forcing the whole subscription back to raw links. Host/external
				// endpoints must fan out here just like raw subscriptions.
				generated := 0
				for _, endpoint := range subReq.naiveShareEndpoints(inbound) {
					forceTLS, _ := endpoint["forceTls"].(string)
					if strings.EqualFold(strings.TrimSpace(forceTLS), "none") {
						continue
					}
					native := s.genNativeNaive(subReq, inbound, client, endpoint)
					if native == nil {
						continue
					}
					tag := client.Email
					if tag == "" {
						tag = fmt.Sprintf("proxy-%d", len(proxies)+1)
					}
					if len(proxies) > 0 {
						tag = fmt.Sprintf("%s-%d", tag, len(proxies)+1)
					}
					native["tag"] = tag
					proxies = append(proxies, nativeOutbound{out: native})
					generated++
				}
				if generated == 0 {
					formatUnsupported = true
				}
				continue
			}
			raws := s.getConfig(subReq, inbound, client, host)
			if len(raws) == 0 && inbound.Protocol != model.WireGuard {
				formatUnsupported = true
				continue
			}
			for _, raw := range raws {
				var xrayCfg map[string]any
				if err := json.Unmarshal(raw, &xrayCfg); err != nil {
					return "", "", err
				}
				outs, _ := xrayCfg["outbounds"].([]any)
				if len(outs) == 0 {
					continue
				}
				proxy, ok := outs[0].(map[string]any)
				if !ok {
					continue
				}
				var native map[string]any
				if nativeType, ok := proxy["type"].(string); ok && nativeType != "" {
					native = proxy
				} else {
					translated, err := singbox.TranslateXrayOutbound(proxy)
					if err != nil {
						formatUnsupported = true
						continue
					}
					native = translated
				}
				tag := client.Email
				if tag == "" {
					tag = fmt.Sprintf("proxy-%d", len(proxies)+1)
				}
				if len(proxies) > 0 {
					tag = fmt.Sprintf("%s-%d", tag, len(proxies)+1)
				}
				native["tag"] = tag
				proxies = append(proxies, nativeOutbound{out: native})
			}
		}
	}

	for _, ext := range externalLinks {
		if ext.Enable {
			hasEnabledClient = true
		}
		if !ext.Active {
			seenEmails[ext.Email] = struct{}{}
			hasInactiveExternal = true
			continue
		}
		for _, el := range expandEntry(ext) {
			outbound := parsedExternalOutbound(el.Link)
			if outbound == nil {
				if shadowTLSForHiddify {
					continue
				}
				return "", "", errSubscriptionFormatUnsupported
			}
			var xrayOutbound map[string]any
			if err := json.Unmarshal(outbound, &xrayOutbound); err != nil {
				return "", "", err
			}
			native, err := singbox.TranslateXrayOutbound(xrayOutbound)
			if err != nil {
				if !shadowTLSForHiddify {
					formatUnsupported = true
				}
				continue
			}
			seenEmails[ext.Email] = struct{}{}
			tag := el.Name
			if tag == "" {
				tag = ext.Email
			}
			if tag == "" {
				tag = fmt.Sprintf("external-%d", len(proxies)+1)
			}
			native["tag"] = tag
			proxies = append(proxies, nativeOutbound{out: native})
		}
	}

	if (formatUnsupported && !shadowTLSForHiddify) || (shadowTLSForHiddify && !shadowTLSGenerated) {
		return "", "", errSubscriptionFormatUnsupported
	}

	if len(proxies) == 0 && !hasInactiveExternal {
		return "", "", nil
	}

	emails := make([]string, 0, len(seenEmails))
	for email := range seenEmails {
		emails = append(emails, email)
	}
	slices.Sort(emails)
	traffic, _ := subReq.AggregateTrafficByEmails(emails)
	traffic.Enable = hasEnabledClient
	header := subReq.subscriptionUserinfo(traffic)

	if mode, remark := subReq.resolveInfoNodeRemark(subId, emails, traffic, len(proxies) > 0); mode != infoNodeNone {
		dummyConfig := s.genDummySocksConfig(remark)
		var dummy map[string]any
		if err := json.Unmarshal(dummyConfig, &dummy); err != nil {
			return "", header, err
		}
		outbounds, ok := dummy["outbounds"].([]any)
		if !ok || len(outbounds) == 0 {
			return "", header, errSubscriptionFormatUnsupported
		}
		proxy, ok := outbounds[0].(map[string]any)
		if !ok {
			return "", header, errSubscriptionFormatUnsupported
		}
		native, err := singbox.TranslateXrayOutbound(proxy)
		if err != nil {
			return "", header, fmt.Errorf("%w: subscription status: %w", errSubscriptionFormatUnsupported, err)
		}
		tag := strings.TrimSpace(remark)
		if tag == "" {
			tag = "subscription-status"
		}
		native["tag"] = tag
		proxies = append([]nativeOutbound{{out: native}}, proxies...)
		if mode == infoNodeExpired || mode == infoNodeDepleted {
			proxies = proxies[:1]
		}
	}

	proxyConfigs := make([]map[string]any, 0, len(proxies))
	for _, proxy := range proxies {
		proxyConfigs = append(proxyConfigs, proxy.out)
	}

	var encoded string
	if hiddify {
		encoded, err = buildHiddifySingBoxSubscription(proxyConfigs)
	} else {
		encoded, err = buildSeparatedSingBoxSubscription(s.bakedTemplate(), proxyConfigs, alwaysReturnArray)
	}
	if err != nil {
		return "", header, err
	}
	return encoded, header, nil
}

func containsSubscriptionProtocol(inbounds []*model.Inbound, protocol model.Protocol) bool {
	for _, inbound := range inbounds {
		if inbound != nil && inbound.Protocol == protocol {
			return true
		}
	}
	return false
}

// subConfigEntry is one ordered block of the JSON subscription: an inbound's
// configs (kind 0) or a balancer config (kind 1).
type subConfigEntry struct {
	sortIndex int
	kind      int
	id        int
	configs   []json_util.RawMessage
}

const (
	subBalancerTag      = "balancer"
	subBalancerProbeURL = "https://www.google.com/generate_204"
)

// subBalancerObservatoryConfig is the panel-wide burstObservatory ping config
// emitted into every client-side balancer doc (subJsonObservatory setting).
type subBalancerObservatoryConfig struct {
	Destination  string `json:"destination"`
	Connectivity string `json:"connectivity"`
	Interval     string `json:"interval"`
	Sampling     int    `json:"sampling"`
	Timeout      string `json:"timeout"`
	HTTPMethod   string `json:"httpMethod"`
}

func defaultSubBalancerObservatoryConfig() subBalancerObservatoryConfig {
	return subBalancerObservatoryConfig{
		Destination:  subBalancerProbeURL,
		Connectivity: "",
		Interval:     "1m",
		Sampling:     2,
		Timeout:      "5s",
		HTTPMethod:   "HEAD",
	}
}

// SetObservatoryConfig overrides defaults from the panel JSON setting. An empty
// cfg keeps all defaults; invalid values fall back with a warning, never panic.
func (s *SubJsonService) SetObservatoryConfig(cfg string) {
	s.observatory = defaultSubBalancerObservatoryConfig()
	if cfg == "" {
		return
	}
	var parsed subBalancerObservatoryConfig
	if err := json.Unmarshal([]byte(cfg), &parsed); err != nil {
		logger.Warningf("subJsonObservatory: invalid JSON %q, using defaults: %v", cfg, err)
		return
	}
	if parsed.Destination != "" {
		if validProbeURL(parsed.Destination) {
			s.observatory.Destination = parsed.Destination
		} else {
			logger.Warningf("subJsonObservatory: invalid destination %q, keeping default %q", parsed.Destination, s.observatory.Destination)
		}
	}
	if parsed.Connectivity != "" {
		if validProbeURL(parsed.Connectivity) {
			s.observatory.Connectivity = parsed.Connectivity
		} else {
			logger.Warningf("subJsonObservatory: invalid connectivity %q, keeping default (skip)", parsed.Connectivity)
		}
	}
	if parsed.Interval != "" {
		if _, err := time.ParseDuration(parsed.Interval); err == nil {
			s.observatory.Interval = parsed.Interval
		} else {
			logger.Warningf("subJsonObservatory: invalid interval %q, keeping default %q", parsed.Interval, s.observatory.Interval)
		}
	}
	if parsed.Sampling > 0 {
		s.observatory.Sampling = parsed.Sampling
	}
	if parsed.Timeout != "" {
		if _, err := time.ParseDuration(parsed.Timeout); err == nil {
			s.observatory.Timeout = parsed.Timeout
		} else {
			logger.Warningf("subJsonObservatory: invalid timeout %q, keeping default %q", parsed.Timeout, s.observatory.Timeout)
		}
	}
	if parsed.HTTPMethod == "HEAD" || parsed.HTTPMethod == "GET" {
		s.observatory.HTTPMethod = parsed.HTTPMethod
	}
}

// validProbeURL accepts only absolute http(s) URLs so a malformed probe or
// connectivity value can't slip into the emitted burstObservatory.
func validProbeURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u == nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func (s *SubJsonService) balancerObservatory(prefix string) map[string]any {
	o := s.observatory
	return map[string]any{
		"subjectSelector": []string{prefix},
		"pingConfig": map[string]any{
			"destination":  o.Destination,
			"connectivity": o.Connectivity,
			"interval":     o.Interval,
			"sampling":     o.Sampling,
			"timeout":      o.Timeout,
			"httpMethod":   o.HTTPMethod,
		},
	}
}

// appendBalancerEntries appends one entry per enabled balancer that has at
// least one member outbound among the inbound entries.
func (s *SubJsonService) appendBalancerEntries(entries []subConfigEntry) []subConfigEntry {
	balancers := getEnabledSubBalancers()
	if len(balancers) == 0 {
		return entries
	}
	// Pre-pass: pull each inbound doc's proxy outbound once so every balancer
	// reuses it instead of re-unmarshalling the whole document per balancer.
	entryProxies := make([][]map[string]any, len(entries))
	for i, entry := range entries {
		if entry.kind != 0 {
			continue
		}
		for _, config := range entry.configs {
			if proxy := extractProxyOutbound(config); proxy != nil {
				entryProxies[i] = append(entryProxies[i], proxy)
			}
		}
	}
	for i := range balancers {
		config := s.buildBalancerConfig(&balancers[i], entries, entryProxies)
		if config == nil {
			continue
		}
		entries = append(entries, subConfigEntry{
			sortIndex: balancers[i].SortOrder,
			kind:      1,
			id:        balancers[i].Id,
			configs:   []json_util.RawMessage{config},
		})
	}
	return entries
}

// extractProxyOutbound returns the first outbound of a document when it is the
// proxy (tag == "proxy"), else nil — the only member shape a balancer retags.
func extractProxyOutbound(config json_util.RawMessage) map[string]any {
	var doc map[string]any
	if json.Unmarshal(config, &doc) != nil {
		return nil
	}
	outbounds, _ := doc["outbounds"].([]any)
	if len(outbounds) == 0 {
		return nil
	}
	outbound, _ := outbounds[0].(map[string]any)
	if outbound == nil || outbound["tag"] != "proxy" {
		return nil
	}
	return outbound
}

func getEnabledSubBalancers() []model.SubBalancer {
	var balancers []model.SubBalancer
	if err := database.GetDB().Model(&model.SubBalancer{}).
		Where("enabled = ?", true).
		Order("sort_order asc, id asc").Find(&balancers).Error; err != nil {
		logger.Error("SubJsonService - getEnabledSubBalancers:", err)
		return nil
	}
	return balancers
}

// Suffix by proxy protocol, not transport network — a vmess/tcp member used to
// be mislabelled "vless".
func balancerMemberSuffix(protocol string) string {
	if protocol == "" {
		return "other"
	}
	return protocol
}

// balMember is one retagged member outbound and the inbound it came from.
type balMember struct {
	tag       string
	inboundId int
}

// leastLoadCosts builds xray's static strategy costs: higher value = picked
// less often; nil unless a member carries an explicit weight (all-1.0 bloat).
func leastLoadCosts(balancer *model.SubBalancer, members []balMember) []any {
	if balancer.Strategy != "leastLoad" || len(members) == 0 || len(balancer.MemberWeights) == 0 {
		return nil
	}
	costs := make([]any, 0, len(members))
	configured := false
	for _, m := range members {
		value := 1.0
		if weight, ok := balancer.MemberWeights[m.inboundId]; ok && weight > 0 {
			value = weight
			configured = true
		}
		// Anchored regexp: plain cost matching is substring-based in xray, so
		// an unanchored "bal-1-vless" would also swallow "bal-1-vless-2".
		costs = append(costs, map[string]any{
			"regexp": true,
			"match":  "^" + m.tag + "$",
			"value":  value,
		})
	}
	if !configured {
		return nil
	}
	return costs
}

// buildBalancerConfig assembles the balancer profile: members retagged under a
// per-balancer prefix, a routing.balancers entry, and (for leastPing/leastLoad) an observatory.
func (s *SubJsonService) buildBalancerConfig(balancer *model.SubBalancer, entries []subConfigEntry, entryProxies [][]map[string]any) json_util.RawMessage {
	prefix := fmt.Sprintf("bal-%d-", balancer.Id)
	usedTags := make(map[string]bool)
	var proxies []json_util.RawMessage
	// Members in emission order with their owning inbound, so costs[] can
	// reference the exact retagged tags assigned here.
	var members []balMember
	var firstTag string
	// entryProxies is the pre-extracted proxy outbounds per entry; kind!=0 rows
	// have none. Clone before retagging so the cached map stays reusable.
	for i, entry := range entries {
		if entry.kind != 0 || !slices.Contains(balancer.InboundIds, entry.id) {
			continue
		}
		for _, outbound := range entryProxies[i] {
			protocol, _ := outbound["protocol"].(string)
			base := prefix + balancerMemberSuffix(protocol)
			tag := base
			for suffix := 2; usedTags[tag]; suffix++ {
				tag = fmt.Sprintf("%s-%d", base, suffix)
			}
			usedTags[tag] = true
			member := maps.Clone(outbound)
			member["tag"] = tag
			if raw, err := json.MarshalIndent(member, "", "  "); err == nil {
				members = append(members, balMember{tag: tag, inboundId: entry.id})
				if firstTag == "" {
					firstTag = tag
				}
				proxies = append(proxies, raw)
			}
		}
	}
	if len(proxies) == 0 {
		return nil
	}

	outbounds := append([]json_util.RawMessage{}, proxies...)
	outbounds = append(outbounds, s.defaultOutbounds...)

	// One template per document: two resolves could straddle a profile refresh
	// and pair this document's dns with the other revision's routing.
	template := s.bakedTemplate()
	// Clone the shared routing subtree (and each rule map) before pointing
	// rules at the balancer.
	baseRouting, _ := template["routing"].(map[string]any)
	routing := make(map[string]any, len(baseRouting)+1)
	maps.Copy(routing, baseRouting)
	baseRules, _ := baseRouting["rules"].([]any)
	rules := make([]any, 0, len(baseRules)+1)
	for _, rule := range baseRules {
		ruleMap, ok := rule.(map[string]any)
		if !ok {
			rules = append(rules, rule)
			continue
		}
		ruleMap = maps.Clone(ruleMap)
		if ruleMap["outboundTag"] == "proxy" {
			delete(ruleMap, "outboundTag")
			ruleMap["balancerTag"] = subBalancerTag
		}
		rules = append(rules, ruleMap)
	}
	routing["rules"] = rules
	isObservatory := balancer.Strategy == "leastPing" || balancer.Strategy == "leastLoad"
	strategyEntry := map[string]any{"type": balancer.Strategy}
	if costs := leastLoadCosts(balancer, members); costs != nil {
		strategyEntry["settings"] = map[string]any{"costs": costs}
	}
	balancerEntry := map[string]any{
		"tag":      subBalancerTag,
		"selector": []string{prefix},
		"strategy": strategyEntry,
	}
	if isObservatory && firstTag != "" {
		// With all probes failing, route to the first member instead of
		// failing dispatch.
		balancerEntry["fallbackTag"] = firstTag
	}
	routing["balancers"] = []any{balancerEntry}

	newConfigJson := make(map[string]any, len(template)+2)
	maps.Copy(newConfigJson, template)
	newConfigJson["outbounds"] = outbounds
	newConfigJson["remarks"] = balancer.Remark
	newConfigJson["routing"] = routing
	// leastPing/leastLoad require a burst observatory (Xray refuses to start
	// them without one); fallbackTag above covers the probe-outage case.
	if isObservatory {
		newConfigJson["burstObservatory"] = s.balancerObservatory(prefix)
	}

	config, _ := json.MarshalIndent(newConfigJson, "", "  ")
	return config
}

func (s *SubJsonService) getConfig(subReq *SubService, inbound *model.Inbound, client model.Client, host string, endpointCopies ...*[]map[string]any) []json_util.RawMessage {
	var newJsonArray []json_util.RawMessage
	stream := s.streamData(inbound.StreamSettings, subKey(client))

	// When externalProxy is empty the JSON config falls back to a
	// synthetic one whose `dest` is the host the client connects to.
	// For node-managed inbounds we want the node's address — request
	// host won't reach the right xray. resolveInboundAddress already
	// implements the node→subscriber-host fallback chain.
	defaultDest := subReq.resolveInboundAddress(inbound)
	if defaultDest == "" {
		defaultDest = host
	}

	// Per-inbound xmux takes precedence over the global subJsonMux.
	// When xmux is present inside xhttpSettings, XHTTP multiplexing
	// is handled by xmux — don't also set the legacy outbound.Mux.
	mux := s.mux
	if xhttp, ok := stream["xhttpSettings"].(map[string]any); ok {
		if _, hasXmux := xhttp["xmux"]; hasXmux {
			mux = ""
		}
	}

	externalProxies, ok := stream["externalProxy"].([]any)
	hasExternalProxy := ok && len(externalProxies) > 0
	if !hasExternalProxy {
		externalProxies = []any{
			map[string]any{
				"forceTls": "same",
				"dest":     defaultDest,
				"port":     float64(inbound.Port),
				"remark":   "",
			},
		}
	}

	delete(stream, "externalProxy")
	network, _ := stream["network"].(string)

	for _, ep := range externalProxies {
		extPrxy, ok := ep.(map[string]any)
		if !ok {
			continue
		}
		// Expand the host's {{VAR}} remark template for this client (no-op for
		// the synthetic/legacy entry) before it's used as the config remark.
		subReq.renderHostRemark(inbound, client, extPrxy, network)
		// Keep the DB-backed inbound immutable while expanding external
		// endpoints. Mutating Listen/Port here leaks the previous endpoint into
		// subsequent generators and can make a different protocol (notably VLESS)
		// connect to the wrong host/port.
		proxyInbound := *inbound
		// External proxy entries are optional overrides. A partial entry must
		// inherit the same advertised endpoint as the synthetic fallback rather
		// than producing an empty host or port 0.
		proxyInbound.Listen, _ = extPrxy["dest"].(string)
		if strings.TrimSpace(proxyInbound.Listen) == "" {
			proxyInbound.Listen = defaultDest
		}
		if port, ok := extPrxy["port"].(float64); ok && int(port) > 0 {
			proxyInbound.Port = int(port)
		}
		newStream := cloneStreamForExternalProxy(stream)
		forceTls, _ := extPrxy["forceTls"].(string)
		switch forceTls {
		case "tls":
			if newStream["security"] != "tls" {
				newStream["security"] = "tls"
				newStream["tlsSettings"] = map[string]any{}
			}
		case "none":
			if newStream["security"] != "none" {
				newStream["security"] = "none"
				delete(newStream, "tlsSettings")
			}
		}
		security, _ := newStream["security"].(string)
		if hasExternalProxy {
			applyExternalProxyTLSToStream(extPrxy, newStream, security)
		}
		applyHostStreamOverrides(extPrxy, newStream)
		hostMux := hostMuxOverride(extPrxy)
		streamSettings, _ := json.MarshalIndent(newStream, "", "  ")

		var newOutbounds []json_util.RawMessage

		switch inbound.Protocol {
		case "vmess":
			newOutbounds = append(newOutbounds, s.genVnext(&proxyInbound, streamSettings, client, jsonMux(mux, hostMux)))
		case "vless":
			vc := client
			vc.ID = applyVlessRoute(client.ID, hostVlessRoute(extPrxy))
			// Same gate the raw link and the Clash proxy apply: a flow left
			// over from a transport Vision supported produces an outbound
			// xray refuses to start.
			newNetwork, _ := newStream["network"].(string)
			if vc.Flow != "" && !vlessFlowAllowed(newNetwork, security, subReq.linkSettings(inbound)) {
				vc.Flow = ""
			}
			newOutbounds = append(newOutbounds, s.genVless(subReq, &proxyInbound, streamSettings, vc, jsonMux(mux, hostMux)))
		case "trojan", "shadowsocks":
			newOutbounds = append(newOutbounds, s.genServer(subReq, &proxyInbound, streamSettings, client, jsonMux(mux, hostMux)))
		case "hysteria":
			// Hysteria is always TLS/QUIC. forceTls=none cannot produce a
			// valid Xray Hysteria client, so skip that external endpoint rather
			// than silently turning it back into TLS inside genHy.
			if forceTls == "none" {
				continue
			}
			// genHy already emits the version-specific Hysteria outbound, including
			// version 2. Do not silently discard Hysteria2 from legacy JSON.
			newOutbounds = append(newOutbounds, s.genHy(&proxyInbound, newStream, client, jsonMux(mux, hostMux)))
		case "wireguard":
			wgOutbound := s.genWireguard(&proxyInbound, client)
			if wgOutbound == nil {
				continue
			}
			newOutbounds = append(newOutbounds, wgOutbound)
		case "tuic", "amneziawg":
			// These protocols do not have an Xray-compatible /json outbound.
			continue
		}

		newOutbounds = append(newOutbounds, s.defaultOutbounds...)
		newConfigJson := make(map[string]any)
		maps.Copy(newConfigJson, s.bakedTemplate())

		transport, _ := newStream["network"].(string)
		newConfigJson["outbounds"] = newOutbounds
		newConfigJson["remarks"] = subReq.endpointRemark(&proxyInbound, client.Email, extPrxy, transport)

		newConfig, _ := json.MarshalIndent(newConfigJson, "", "  ")
		newJsonArray = append(newJsonArray, newConfig)
		if len(endpointCopies) > 0 && endpointCopies[0] != nil {
			*endpointCopies[0] = append(*endpointCopies[0], maps.Clone(extPrxy))
		}
	}

	return newJsonArray
}

func (s *SubJsonService) streamData(stream string, clientKey string) map[string]any {
	var streamSettings map[string]any
	if err := json.Unmarshal([]byte(stream), &streamSettings); err != nil || streamSettings == nil {
		streamSettings = map[string]any{}
	}
	security, _ := streamSettings["security"].(string)
	switch security {
	case "tls":
		if tlsSettings, ok := streamSettings["tlsSettings"].(map[string]any); ok {
			streamSettings["tlsSettings"] = s.tlsData(tlsSettings)
		} else {
			delete(streamSettings, "tlsSettings")
		}
	case "reality":
		if realitySettings, ok := streamSettings["realitySettings"].(map[string]any); ok {
			streamSettings["realitySettings"] = s.realityData(realitySettings, clientKey)
		} else {
			delete(streamSettings, "realitySettings")
		}
	}
	delete(streamSettings, "sockopt")

	if s.finalMask != "" {
		s.applyGlobalFinalMask(streamSettings)
	}

	// remove proxy protocol
	network, _ := streamSettings["network"].(string)
	switch network {
	case "tcp":
		streamSettings["tcpSettings"] = s.removeAcceptProxy(streamSettings["tcpSettings"])
	case "ws":
		streamSettings["wsSettings"] = s.removeAcceptProxy(streamSettings["wsSettings"])
	case "httpupgrade":
		streamSettings["httpupgradeSettings"] = s.removeAcceptProxy(streamSettings["httpupgradeSettings"])
	case "xhttp":
		streamSettings["xhttpSettings"] = s.removeAcceptProxy(streamSettings["xhttpSettings"])
		if xhttp, ok := streamSettings["xhttpSettings"].(map[string]any); ok {
			delete(xhttp, "noSSEHeader")
			delete(xhttp, "scMaxBufferedPosts")
			delete(xhttp, "scStreamUpServerSecs")
			delete(xhttp, "serverMaxHeaderBytes")
			// Values matching xray-core's own defaults stay off the wire:
			// old panels seeded them into every stored config and the
			// literal scMinPostsIntervalMs=30 is a DPI fingerprint (#5141).
			if v, _ := xhttp["scMaxEachPostBytes"].(string); v == "" || v == "1000000" {
				delete(xhttp, "scMaxEachPostBytes")
			}
			if v, _ := xhttp["scMinPostsIntervalMs"].(string); v == "" || v == "30" {
				delete(xhttp, "scMinPostsIntervalMs")
			}
		}
	}
	return streamSettings
}

func (s *SubJsonService) applyGlobalFinalMask(streamSettings map[string]any) {
	var fm map[string]any
	if err := json.Unmarshal([]byte(s.finalMask), &fm); err != nil || len(fm) == 0 {
		return
	}
	merged := mergeFinalMask(streamSettings["finalmask"], fm)
	if len(merged) > 0 {
		streamSettings["finalmask"] = merged
	}
}

func (s *SubJsonService) removeAcceptProxy(setting any) map[string]any {
	netSettings, ok := setting.(map[string]any)
	if ok {
		delete(netSettings, "acceptProxyProtocol")
	}
	return netSettings
}

func (s *SubJsonService) tlsData(tData map[string]any) map[string]any {
	tlsData := make(map[string]any, 1)
	tlsClientSettings, _ := tData["settings"].(map[string]any)

	tlsData["serverName"] = tData["serverName"]
	tlsData["alpn"] = tData["alpn"]
	if fingerprint, ok := tlsClientSettings["fingerprint"].(string); ok {
		tlsData["fingerprint"] = fingerprint
	}
	if cs, ok := tData["cipherSuites"].(string); ok && cs != "" {
		tlsData["cipherSuites"] = cs
	}
	if ech, ok := tlsClientSettings["echConfigList"].(string); ok && ech != "" {
		tlsData["echConfigList"] = ech
	}
	if vcn, ok := verifyPeerCertByNameValue(tlsClientSettings); ok {
		tlsData["verifyPeerCertByName"] = vcn
	}
	// xray-core now parses pinnedPeerCertSha256 as a comma-separated string, not
	// an array; emit the joined form so v2ray clients can import the config (#5401).
	if pins, ok := pinnedSha256List(tlsClientSettings); ok {
		tlsData["pinnedPeerCertSha256"] = strings.Join(pins, ",")
	}
	return tlsData
}

func (s *SubJsonService) realityData(rData map[string]any, clientKey string) map[string]any {
	rltyData := make(map[string]any, 1)
	rltyClientSettings, _ := rData["settings"].(map[string]any)

	rltyData["show"] = false
	rltyData["publicKey"] = rltyClientSettings["publicKey"]
	rltyData["fingerprint"] = rltyClientSettings["fingerprint"]
	rltyData["mldsa65Verify"] = rltyClientSettings["mldsa65Verify"]

	seed, _ := rltyClientSettings["spiderX"].(string)
	rltyData["spiderX"] = deriveSpiderX(seed, clientKey)
	shortIds, ok := rData["shortIds"].([]any)
	if ok && len(shortIds) > 0 {
		rltyData["shortId"], _ = shortIds[random.Num(len(shortIds))].(string)
	} else {
		rltyData["shortId"] = ""
	}
	serverNames, ok := rData["serverNames"].([]any)
	if name, _ := rltyClientSettings["serverName"].(string); name != "" {
		rltyData["serverName"] = name
	} else if ok && len(serverNames) > 0 {
		rltyData["serverName"], _ = serverNames[random.Num(len(serverNames))].(string)
	} else {
		rltyData["serverName"] = ""
	}

	return rltyData
}

// jsonMux picks the per-host mux override when present, else the global mux.
func jsonMux(global, override string) string {
	if override != "" {
		return override
	}
	return global
}

func (s *SubJsonService) genVnext(inbound *model.Inbound, streamSettings json_util.RawMessage, client model.Client, mux string) json_util.RawMessage {
	outbound := Outbound{
		Protocol: string(inbound.Protocol),
		Tag:      "proxy",
	}
	if mux != "" {
		outbound.Mux = json_util.RawMessage(mux)
	}
	outbound.StreamSettings = streamSettings

	security := normalizeVmessSecurity(client.Security)
	outbound.Settings = map[string]any{
		"address":  inbound.Listen,
		"port":     inbound.Port,
		"id":       client.ID,
		"security": security,
		"level":    8,
	}

	result, _ := json.MarshalIndent(outbound, "", "  ")
	return result
}

func (s *SubJsonService) genVless(subReq *SubService, inbound *model.Inbound, streamSettings json_util.RawMessage, client model.Client, mux string) json_util.RawMessage {
	outbound := Outbound{
		Protocol: string(inbound.Protocol),
		Tag:      "proxy",
	}
	if mux != "" {
		outbound.Mux = json_util.RawMessage(mux)
	}
	outbound.StreamSettings = streamSettings

	// Add encryption for VLESS outbound from inbound settings
	inboundSettings := subReq.linkSettings(inbound)
	encryption := "none"
	if configured, ok := inboundSettings["encryption"].(string); ok && strings.TrimSpace(configured) != "" {
		encryption = strings.TrimSpace(configured)
	}

	settings := map[string]any{
		"address":    inbound.Listen,
		"port":       inbound.Port,
		"id":         client.ID,
		"encryption": encryption,
		"level":      8,
	}
	if client.Flow != "" && !inbound.DisableFlow {
		settings["flow"] = client.Flow
		outbound.Mux = muxWithoutTCP(mux)
	}
	outbound.Settings = settings
	result, _ := json.MarshalIndent(outbound, "", "  ")
	return result
}

// XTLS flows reject TCP mux.cool ("unexpected network TCP"); concurrency -1
// turns only that off and keeps the XUDP keys (Xray reads them under enabled).
func muxWithoutTCP(mux string) json_util.RawMessage {
	if mux == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(mux), &m); err != nil || m == nil {
		return nil
	}
	m["concurrency"] = -1
	out, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return json_util.RawMessage(out)
}

func (s *SubJsonService) genServer(subReq *SubService, inbound *model.Inbound, streamSettings json_util.RawMessage, client model.Client, mux string) json_util.RawMessage {
	outbound := Outbound{}

	serverData := make([]ServerSetting, 1)
	serverData[0] = ServerSetting{
		Address:  inbound.Listen,
		Port:     inbound.Port,
		Level:    8,
		Password: client.Password,
	}

	if inbound.Protocol == model.Shadowsocks {
		inboundSettings := subReq.linkSettings(inbound)
		method, _ := inboundSettings["method"].(string)
		serverData[0].Method = method

		// server password in multi-user 2022 protocols
		if strings.HasPrefix(method, "2022") {
			if serverPassword, ok := inboundSettings["password"].(string); ok {
				serverData[0].Password = fmt.Sprintf("%s:%s", serverPassword, client.Password)
			}
		}
	}

	outbound.Protocol = string(inbound.Protocol)
	outbound.Tag = "proxy"
	if mux != "" {
		outbound.Mux = json_util.RawMessage(mux)
	}
	outbound.StreamSettings = streamSettings

	// Wrap the endpoint in a "servers" array (the standard Xray schema for
	// Shadowsocks/Trojan outbounds). The flat top-level form only parses on very
	// recent xray-core; older bundled cores (e.g. in v2rayN) reject it, so SS
	// links fail to connect. See genVnext/genVless for the VMess/VLESS shape.
	server := map[string]any{
		"address":  serverData[0].Address,
		"port":     serverData[0].Port,
		"password": serverData[0].Password,
		"level":    8,
	}
	if inbound.Protocol == model.Shadowsocks {
		server["method"] = serverData[0].Method
	}
	outbound.Settings = map[string]any{
		"servers": []any{server},
	}

	result, _ := json.MarshalIndent(outbound, "", "  ")
	return result
}

func (s *SubJsonService) genNativeTLSLike(subReq *SubService, inbound *model.Inbound, client model.Client) map[string]any {
	return s.genNativeTLSLikeEndpoint(subReq, inbound, client, subReq.inboundDefaultEndpoint(inbound))
}

func containsShadowTLSTransport(inbounds []*model.Inbound) bool {
	for _, inbound := range inbounds {
		if inbound != nil && model.ShadowTLSTransport(inbound.Settings) != nil {
			return true
		}
	}
	return false
}

// Attach the native ShadowTLS outbound to an inner TCP proxy. The inner
// protocol still owns per-client credentials; ShadowTLS owns the public dial.
func (s *SubJsonService) wrapNativeShadowTLS(inbound *model.Inbound, inner map[string]any, endpoint map[string]any) bool {
	if inner == nil {
		return false
	}
	transport := model.ShadowTLSTransport(inbound.Settings)
	password, _ := transport["password"].(string)
	server, _ := inner["server"].(string)
	port := inner["server_port"]
	if server == "" || port == nil || password == "" {
		return false
	}
	handshake, _ := transport["handshake"].(map[string]any)
	sni, _ := handshake["server"].(string)
	if strings.TrimSpace(sni) == "" {
		sni = "cloudflare.com"
	}
	if endpoint != nil {
		if override, ok := externalProxySNI(endpoint); ok && strings.TrimSpace(override) != "" {
			wildcard, _ := transport["wildcardSni"].(string)
			named, _ := transport["handshakeForServerName"].(map[string]any)
			hasNamedSNI := false
			for name := range named {
				if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(override)) {
					hasNamedSNI = true
					break
				}
			}
			if wildcard == "all" || wildcard == "authed" || net.ParseIP(sni) != nil || hasNamedSNI {
				sni = strings.TrimSpace(override)
			}
		}
	}
	tls := map[string]any{"enabled": true, "server_name": sni}
	applyNativeTLSHostOptions(tls, endpoint)
	tag := "§hide§ shadowtls-transport"
	inner["detour"] = tag
	delete(inner, "flow")
	inner["_panel_shadowtls_transport"] = map[string]any{
		"type": "shadowtls", "tag": tag, "server": server,
		"server_port": port, "version": 3, "password": password, "tls": tls,
	}
	return true
}

func (s *SubJsonService) genNativeTLSLikeEndpoint(subReq *SubService, inbound *model.Inbound, client model.Client, endpoint ShareEndpoint) map[string]any {
	if (inbound.Protocol != model.AnyTLS && inbound.Protocol != model.ShadowTLS) || client.Password == "" ||
		(inbound.Protocol == model.ShadowTLS && client.Email == "") {
		return nil
	}

	server := strings.TrimSpace(endpoint.Address)
	if server == "" {
		server = strings.TrimSpace(subReq.resolveInboundAddress(inbound))
	}
	if server == "" {
		server = strings.TrimSpace(inbound.Listen)
	}
	port := endpoint.Port
	if port <= 0 {
		port = inbound.Port
	}
	if server == "" || port <= 0 {
		return nil
	}

	settings := subReq.linkSettings(inbound)
	masterKey, _ := settings[model.ShadowTLSInnerKeyField].(string)
	if inbound.Protocol == model.ShadowTLS && !model.ValidShadowTLSInnerKey(masterKey) {
		return nil
	}
	if inbound.Protocol == model.AnyTLS {
		out := map[string]any{
			"type":        "anytls",
			"server":      server,
			"server_port": port,
			"password":    client.Password,
		}
		tls := map[string]any{"enabled": true}
		if rawTLS, ok := settings["tls"].(map[string]any); ok {
			if serverName, ok := rawTLS["serverName"].(string); ok && strings.TrimSpace(serverName) != "" {
				tls["server_name"] = strings.TrimSpace(serverName)
			}
		}
		if endpoint.ep != nil {
			if sni, ok := externalProxySNI(endpoint.ep); ok {
				tls["server_name"] = sni
			}
		}
		applyNativeTLSHostOptions(tls, endpoint.ep)
		out["tls"] = tls
		return out
	}

	handshake, _ := settings["handshake"].(map[string]any)
	handshakeServer, _ := handshake["server"].(string)
	handshakeServer = strings.TrimSpace(handshakeServer)
	if handshakeServer == "" {
		handshakeServer, _ = handshake["address"].(string)
		handshakeServer = strings.TrimSpace(handshakeServer)
	}
	if handshakeServer == "" {
		// With wildcard_sni=all the server may omit its fallback handshake
		// target, but the client still needs a concrete SNI for the TLS hello.
		handshakeServer = "cloudflare.com"
	}
	if endpoint.ep != nil {
		if sni, ok := externalProxySNI(endpoint.ep); ok {
			sni = strings.TrimSpace(sni)
			wildcard, _ := settings["wildcardSni"].(string)
			named, _ := settings["handshakeForServerName"].(map[string]any)
			namedSNI := false
			for name := range named {
				if strings.EqualFold(strings.TrimSpace(name), sni) {
					namedSNI = true
					break
				}
			}
			if sni != "" && (wildcard == "all" || wildcard == "authed" || net.ParseIP(handshakeServer) != nil || namedSNI) {
				handshakeServer = sni
			}
		}
	}
	tls := map[string]any{"enabled": true, "server_name": handshakeServer}
	applyNativeTLSHostOptions(tls, endpoint.ep)
	transportTag := "§hide§ shadowtls-transport"
	transport := map[string]any{
		"type":        "shadowtls",
		"tag":         transportTag,
		"server":      server,
		"server_port": port,
		"version":     3,
		"password":    client.Password,
		"tls":         tls,
	}
	// Match Hiddify's ShadowTLS + Shadowsocks 2022 detour: the inner protocol
	// carries the destination and encrypts the payload after the TLS handshake.
	return map[string]any{
		"type": "shadowsocks", "method": "2022-blake3-aes-128-gcm",
		"password":                   masterKey + ":" + model.ShadowTLSClientKey(client.Email, client.Password),
		"udp_over_tcp":               map[string]any{"enabled": true, "version": 2},
		"detour":                     transportTag,
		"_panel_shadowtls_transport": transport,
	}
}

func applyNativeTLSHostOptions(tls map[string]any, endpoint map[string]any) {
	if endpoint == nil {
		return
	}
	if insecure, _ := endpoint["allowInsecure"].(bool); insecure {
		tls["insecure"] = true
	}
	if alpn, ok := endpoint["alpn"].([]any); ok && len(alpn) > 0 {
		tls["alpn"] = alpn
	}
}

func (s *SubJsonService) genNativeNaive(subReq *SubService, inbound *model.Inbound, client model.Client, endpoint map[string]any) map[string]any {
	if inbound.Protocol != model.NaiveProxy || client.Email == "" || client.Password == "" {
		return nil
	}

	settings := subReq.linkSettings(inbound)
	network, _ := settings["network"].(string)
	serverName := ""
	if tlsSettings, ok := settings["tls"].(map[string]any); ok {
		serverName, _ = tlsSettings["serverName"].(string)
	}
	serverName = strings.TrimSpace(serverName)

	server := subReq.resolveInboundAddress(inbound)
	serverPort := inbound.Port
	if endpoint != nil {
		if dest, ok := endpoint["dest"].(string); ok && strings.TrimSpace(dest) != "" {
			server = strings.TrimSpace(dest)
		}
		if rawPort, ok := endpoint["port"].(float64); ok && int(rawPort) > 0 {
			serverPort = int(rawPort)
		} else if rawPort, ok := endpoint["port"].(int); ok && rawPort > 0 {
			serverPort = rawPort
		}
		if sni, ok := externalProxySNI(endpoint); ok {
			serverName = strings.TrimSpace(sni)
		}
	}
	blankSNI, _ := endpoint["keepSniBlank"].(bool)
	if serverName == "" && !blankSNI {
		if server != "" && !strings.Contains(server, ":") && net.ParseIP(server) == nil {
			serverName = server
		} else {
			serverName = subReq.configuredPublicHost()
		}
	}
	if server == "" || serverPort <= 0 {
		return nil
	}

	out := map[string]any{
		"type":         "naive",
		"server":       server,
		"server_port":  serverPort,
		"username":     client.Email,
		"password":     client.Password,
		"udp_over_tcp": true,
		"quic":         strings.EqualFold(strings.TrimSpace(network), "udp"),
	}

	if cc, ok := settings["quicCongestionControl"].(string); ok && strings.TrimSpace(cc) != "" {
		out["quic_congestion_control"] = strings.TrimSpace(cc)
	}
	if host, ok := endpoint["hostHeader"].(string); ok && strings.TrimSpace(host) != "" {
		out["extra_headers"] = map[string]string{"Host": strings.TrimSpace(host)}
	}
	tls := map[string]any{"enabled": true}
	if serverName != "" {
		tls["server_name"] = serverName
	}
	out["tls"] = tls
	return out
}

func (s *SubJsonService) genNativeTUIC(inbound *model.Inbound, stream map[string]any, client model.Client) json_util.RawMessage {
	settings := map[string]any{
		"servers": []any{map[string]any{
			"address":  inbound.Listen,
			"port":     inbound.Port,
			"id":       client.ID,
			"uuid":     client.ID,
			"password": client.Password,
		}},
	}
	// These are client-side TUIC knobs stored with the inbound and must survive
	// conversion into a native sing-box outbound. Without this, structured
	// subscriptions silently fall back to sing-box defaults.
	if inst, ok := tuic.InstanceFromInbound(inbound); ok {
		settings["congestion_control"] = inst.CongestionControl
		settings["udp_relay_mode"] = inst.UDPRelayMode
		settings["zero_rtt_handshake"] = inst.ZeroRTTHandshake
		stream = maps.Clone(stream)
		tls, _ := stream["tlsSettings"].(map[string]any)
		tls = maps.Clone(tls)
		if tls == nil {
			tls = map[string]any{}
		}
		if len(inst.ALPN) > 0 {
			tls["alpn"] = inst.ALPN
		}
		stream["tlsSettings"] = tls
		stream["security"] = "tls"
	}
	raw := map[string]any{
		"protocol":       "tuic",
		"tag":            "proxy",
		"settings":       settings,
		"streamSettings": stream,
	}
	translated, err := singbox.TranslateXrayOutbound(raw)
	if err != nil {
		return nil
	}
	translated["tag"] = "proxy"
	b, _ := json.MarshalIndent(translated, "", "  ")
	return b
}

func (s *SubJsonService) genHy(inbound *model.Inbound, newStream map[string]any, client model.Client, mux string) json_util.RawMessage {
	outbound := Outbound{
		Protocol: string(inbound.Protocol),
		Tag:      "proxy",
	}

	if mux != "" {
		outbound.Mux = json_util.RawMessage(mux)
	}

	var settings, stream map[string]any
	_ = json.Unmarshal([]byte(inbound.Settings), &settings)
	version, _ := settings["version"].(float64)
	outbound.Settings = map[string]any{
		"version": int(version),
		"address": inbound.Listen,
		"port":    inbound.Port,
	}

	_ = json.Unmarshal([]byte(inbound.StreamSettings), &stream)
	hyStream, _ := stream["hysteriaSettings"].(map[string]any)
	outHyStream := map[string]any{
		"version": int(version),
		"auth":    client.Auth,
	}
	if udpIdleTimeout, ok := hyStream["udpIdleTimeout"].(float64); ok {
		outHyStream["udpIdleTimeout"] = int(udpIdleTimeout)
	}
	if masquerade, ok := hyStream["masquerade"].(map[string]any); ok {
		outHyStream["masquerade"] = masquerade
	}
	for _, key := range []string{
		"up_mbps", "down_mbps", "hop_interval", "hop_interval_max",
		"upMbps", "downMbps", "up", "down", "hopInterval", "hopIntervalMax",
		"bbrProfile", "disableChromeParrot", "ignoreClientBandwidth",
		"bbr_profile", "disable_chrome_parrot", "ignore_client_bandwidth",
	} {
		if value, ok := hyStream[key]; ok {
			outHyStream[key] = value
		}
	}
	if obfs, ok := hyStream["obfs"]; ok {
		outHyStream["obfs"] = obfs
	}
	newStream["hysteriaSettings"] = outHyStream

	if finalmask, ok := hyStream["finalmask"].(map[string]any); ok {
		newStream["finalmask"] = mergeFinalMask(newStream["finalmask"], finalmask)
	}

	newStream["network"] = "hysteria"
	newStream["security"] = "tls"

	outbound.StreamSettings, _ = json.MarshalIndent(newStream, "", "  ")

	result, _ := json.MarshalIndent(outbound, "", "  ")
	return result
}

// wireguardPeerAddress returns the advertised endpoint rather than the inbound bind address.
func wireguardPeerAddress(inbound *model.Inbound, host string, resolve func(*model.Inbound) string) string {
	address := ""
	if resolve != nil {
		address = strings.TrimSpace(resolve(inbound))
	}
	if address == "" || address == "0.0.0.0" || address == "::" || address == "[::]" || address == "localhost" {
		address = strings.TrimSpace(host)
	}
	return address
}

// genWireguard builds an Xray-compatible WireGuard outbound for the legacy
// /json subscription. Native sing-box subscriptions translate WireGuard separately.
func (s *SubJsonService) genWireguard(inbound *model.Inbound, client model.Client) json_util.RawMessage {
	if client.PrivateKey == "" {
		return nil
	}
	var settings map[string]any
	_ = json.Unmarshal([]byte(inbound.Settings), &settings)

	serverPrivateKey, _ := settings["secretKey"].(string)

	addresses := append([]string(nil), client.AllowedIPs...)
	peer := map[string]any{
		"endpoint":   joinHostPort(wireguardPeerAddress(inbound, inbound.Listen, nil), inbound.Port),
		"allowedIPs": []string{"0.0.0.0/0", "::/0"},
	}
	if serverPrivateKey != "" {
		if publicKey, err := wgutil.PublicKeyFromPrivate(serverPrivateKey); err == nil {
			peer["publicKey"] = publicKey
		}
	}
	if client.PreSharedKey != "" {
		peer["preSharedKey"] = client.PreSharedKey
	}
	if ka := client.KeepAliveSeconds(); ka > 0 {
		peer["keepAlive"] = ka
	}

	settingsOut := map[string]any{
		"secretKey": client.PrivateKey,
		"peers":     []any{peer},
	}
	if len(addresses) > 0 {
		settingsOut["address"] = addresses
	}
	outbound := map[string]any{
		"protocol": "wireguard",
		"tag":      "proxy",
		"settings": settingsOut,
	}
	if mtu, ok := settings["mtu"].(float64); ok && mtu > 0 {
		outbound["settings"].(map[string]any)["mtu"] = int(mtu)
	}
	result, _ := json.MarshalIndent(outbound, "", "  ")
	return result
}

func (s *SubJsonService) genDummySocksConfig(remark string) json_util.RawMessage {
	outbound := map[string]any{
		"protocol": "socks",
		"tag":      "proxy",
		"settings": map[string]any{
			"servers": []any{
				map[string]any{
					"address": "127.0.0.1",
					"port":    1080,
				},
			},
		},
	}
	rawOutbound, _ := json.Marshal(outbound)
	newOutbounds := []json_util.RawMessage{rawOutbound}
	newOutbounds = append(newOutbounds, s.defaultOutbounds...)

	newConfigJson := make(map[string]any)
	maps.Copy(newConfigJson, s.configJson)
	if s.dnsBlock != nil {
		newConfigJson["dns"] = s.dnsBlock
	}
	newConfigJson["outbounds"] = newOutbounds
	newConfigJson["remarks"] = remark

	newConfig, _ := json.MarshalIndent(newConfigJson, "", "  ")
	return newConfig
}

func mergeFinalMask(base any, extra map[string]any) map[string]any {
	merged := map[string]any{}
	if baseMap, ok := base.(map[string]any); ok {
		for key, value := range baseMap {
			switch key {
			case "tcp", "udp":
				if masks, ok := value.([]any); ok {
					merged[key] = append([]any(nil), masks...)
				}
			default:
				merged[key] = value
			}
		}
	}

	for key, value := range extra {
		switch key {
		case "tcp", "udp":
			baseMasks, _ := merged[key].([]any)
			extraMasks, _ := value.([]any)
			if len(extraMasks) > 0 {
				merged[key] = append(baseMasks, extraMasks...)
			}
		case "quicParams":
			if _, exists := merged[key]; !exists {
				merged[key] = value
			}
		default:
			merged[key] = value
		}
	}

	return merged
}

type Outbound struct {
	Protocol       string               `json:"protocol"`
	Tag            string               `json:"tag"`
	StreamSettings json_util.RawMessage `json:"streamSettings"`
	Mux            json_util.RawMessage `json:"mux,omitempty"`
	Settings       map[string]any       `json:"settings,omitempty"`
}

type ServerSetting struct {
	Password string `json:"password"`
	Level    int    `json:"level"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Flow     string `json:"flow,omitempty"`
	Method   string `json:"method,omitempty"`
}
