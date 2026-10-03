package outbound

import (
	"fmt"
	"strings"
)

// Keep the tested targets and their complete dependency closure. An unrelated
// broken outbound must not poison every probe or every per-item retry.
func selectProbeOutbounds(outbounds []map[string]any, roots []string) ([]map[string]any, error) {
	byTag := make(map[string]map[string]any, len(outbounds))
	duplicates := make(map[string]bool)
	for _, outbound := range outbounds {
		tag, _ := outbound["tag"].(string)
		if tag == "" {
			continue
		}
		if _, exists := byTag[tag]; exists {
			duplicates[tag] = true
			continue
		}
		byTag[tag] = outbound
	}
	state := make(map[string]uint8)
	var visit func(string, []string) error
	visit = func(tag string, path []string) error {
		if duplicates[tag] {
			return fmt.Errorf("duplicate outbound tag %q in probe context", tag)
		}
		outbound, exists := byTag[tag]
		if !exists {
			return fmt.Errorf("outbound dependency %q does not exist (chain: %s)", tag, strings.Join(append(path, tag), " -> "))
		}
		if state[tag] == 1 {
			return fmt.Errorf("outbound dependency cycle: %s", strings.Join(append(path, tag), " -> "))
		}
		if state[tag] == 2 {
			return nil
		}
		state[tag] = 1
		path = append(path, tag)
		for _, dependency := range probeOutboundDependencies(outbound) {
			if err := visit(dependency, path); err != nil {
				return err
			}
		}
		state[tag] = 2
		return nil
	}
	for _, root := range roots {
		if err := visit(root, nil); err != nil {
			return nil, err
		}
	}
	selected := make([]map[string]any, 0, len(state))
	for _, outbound := range outbounds {
		tag, _ := outbound["tag"].(string)
		if state[tag] == 2 {
			selected = append(selected, outbound)
		}
	}
	return selected, nil
}

func probeOutboundDependencies(outbound map[string]any) []string {
	var dependencies []string
	add := func(value any) {
		if tag, ok := value.(string); ok && tag != "" {
			dependencies = append(dependencies, tag)
		}
	}
	stream, _ := outbound["streamSettings"].(map[string]any)
	sockopt, _ := stream["sockopt"].(map[string]any)
	dialer, _ := sockopt["dialerProxy"].(string)
	dialer = strings.TrimSpace(dialer)
	protocol, _ := outbound["protocol"].(string)
	settings, _ := outbound["settings"].(map[string]any)
	if dialer != "" {
		add(dialer)
	} else {
		add(settings["detour"])
	}
	if normalizedProbeProtocol(protocol) == "selector" || normalizedProbeProtocol(protocol) == "urltest" {
		switch members := settings["outbounds"].(type) {
		case []any:
			for _, member := range members {
				add(member)
			}
		case []string:
			for _, member := range members {
				add(member)
			}
		}
	}
	return dependencies
}
