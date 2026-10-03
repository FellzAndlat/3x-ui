package adblock

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const OutboundTag = "3x-ui-adblock"

var ErrOutboundTagConflict = errors.New("adblock outbound tag already exists with a non-blackhole protocol")

// ApplyXrayRouting injects a high-priority domain block rule and the matching
// blackhole outbound into Xray JSON fragments. Calling it repeatedly is
// idempotent. Passing an empty domain slice removes the managed rule/outbound.
func ApplyXrayRouting(routerJSON, outboundsJSON []byte, domains []string) ([]byte, []byte, error) {
	router, err := decodeObject(routerJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("decode routing: %w", err)
	}
	outbounds, err := decodeArray(outboundsJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("decode outbounds: %w", err)
	}

	normalized := normalizeDomainSlice(domains)
	outbounds, err = updateOutbounds(outbounds, len(normalized) > 0)
	if err != nil {
		return nil, nil, err
	}
	if err := updateRules(router, normalized); err != nil {
		return nil, nil, err
	}

	routerResult, err := json.Marshal(router)
	if err != nil {
		return nil, nil, fmt.Errorf("encode routing: %w", err)
	}
	outboundResult, err := json.Marshal(outbounds)
	if err != nil {
		return nil, nil, fmt.Errorf("encode outbounds: %w", err)
	}
	return routerResult, outboundResult, nil
}

func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	if len(data) == 0 || string(data) == "null" {
		return make(map[string]json.RawMessage), nil
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = make(map[string]json.RawMessage)
	}
	return result, nil
}

func decodeArray(data []byte) ([]json.RawMessage, error) {
	if len(data) == 0 || string(data) == "null" {
		return []json.RawMessage{}, nil
	}
	var result []json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = []json.RawMessage{}
	}
	return result, nil
}

func updateOutbounds(outbounds []json.RawMessage, enabled bool) ([]json.RawMessage, error) {
	result := make([]json.RawMessage, 0, len(outbounds)+1)
	found := false
	hasUserOutbound := false
	for _, raw := range outbounds {
		var meta struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("decode outbound: %w", err)
		}
		if meta.Tag != OutboundTag {
			hasUserOutbound = true
			result = append(result, raw)
			continue
		}
		found = true
		if meta.Protocol != "blackhole" {
			return nil, ErrOutboundTagConflict
		}
		if enabled {
			result = append(result, raw)
		}
	}
	if enabled && !hasUserOutbound {
		// Xray's implicit default is freedom when no outbounds are configured.
		// Keep that default first so unrelated traffic never falls into blackhole.
		result = append([]json.RawMessage{json.RawMessage(`{"protocol":"freedom"}`)}, result...)
	}
	if enabled && !found {
		raw, _ := json.Marshal(map[string]any{
			"tag":      OutboundTag,
			"protocol": "blackhole",
		})
		result = append(result, raw)
	}
	return result, nil
}

func updateRules(router map[string]json.RawMessage, domains []string) error {
	var rules []json.RawMessage
	if raw, ok := router["rules"]; ok && len(raw) != 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &rules); err != nil {
			return fmt.Errorf("decode routing rules: %w", err)
		}
	}

	filtered := make([]json.RawMessage, 0, len(rules)+1)
	for _, raw := range rules {
		var meta struct {
			OutboundTag string `json:"outboundTag"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			return fmt.Errorf("decode routing rule: %w", err)
		}
		if meta.OutboundTag != OutboundTag {
			filtered = append(filtered, raw)
		}
	}

	if len(domains) > 0 {
		xrayDomains := make([]string, 0, len(domains))
		for _, domain := range domains {
			if strings.HasPrefix(domain, "domain:") {
				xrayDomains = append(xrayDomains, domain)
			} else {
				xrayDomains = append(xrayDomains, "full:"+domain)
			}
		}
		rule, _ := json.Marshal(map[string]any{
			"type":        "field",
			"domain":      xrayDomains,
			"outboundTag": OutboundTag,
		})
		filtered = append([]json.RawMessage{rule}, filtered...)
	}

	rawRules, err := json.Marshal(filtered)
	if err != nil {
		return fmt.Errorf("encode routing rules: %w", err)
	}
	router["rules"] = rawRules
	return nil
}

func normalizeDomainSlice(domains []string) []string {
	set := make(map[string]struct{}, len(domains))
	for _, value := range domains {
		if domain, ok := normalizeDomain(value); ok {
			if strings.HasPrefix(strings.ToLower(value), "domain:") {
				domain = "domain:" + domain
			}
			set[domain] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for domain := range set {
		result = append(result, domain)
	}
	sort.Strings(result)
	return result
}
