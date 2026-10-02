package singbox

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// normalizeRouteForRuntime converts panel-friendly route values into the
// strict sing-box JSON schema without mutating the stored editor template.
// The UI keeps comments and a compact comma-separated port field for editing;
// sing-box itself accepts neither the comment field nor mixed port/range text.
func normalizeRouteForRuntime(route map[string]any) (map[string]any, error) {
	if route == nil {
		return nil, nil
	}

	data, err := json.Marshal(route)
	if err != nil {
		return nil, fmt.Errorf("clone route config: %w", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, fmt.Errorf("clone route config: %w", err)
	}

	if rawRules, exists := normalized["rules"]; exists {
		rules, err := normalizeRuntimeRuleList(rawRules, "route.rules")
		if err != nil {
			return nil, err
		}
		normalized["rules"] = rules
	}
	return normalized, nil
}

func normalizeRuntimeRuleList(value any, path string) ([]any, error) {
	rules, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", path)
	}

	for index, rawRule := range rules {
		rule, ok := rawRule.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be an object", path, index)
		}

		// comment is panel-only metadata. sing-box uses strict JSON decoding and
		// rejects unknown route-rule fields.
		delete(rule, "comment")

		if err := normalizeRuntimePortField(rule, "port", "port_range", fmt.Sprintf("%s[%d]", path, index)); err != nil {
			return nil, err
		}
		if err := normalizeRuntimePortField(rule, "source_port", "source_port_range", fmt.Sprintf("%s[%d]", path, index)); err != nil {
			return nil, err
		}

		// Logical route rules contain another rules array. Normalize recursively
		// so panel metadata never leaks through nested AND/OR groups either.
		if nested, exists := rule["rules"]; exists {
			normalizedNested, err := normalizeRuntimeRuleList(nested, fmt.Sprintf("%s[%d].rules", path, index))
			if err != nil {
				return nil, err
			}
			rule["rules"] = normalizedNested
		}
	}
	return rules, nil
}

func normalizeRuntimePortField(rule map[string]any, field, rangeField, path string) error {
	raw, exists := rule[field]
	if !exists {
		return nil
	}
	text, ok := raw.(string)
	if !ok {
		// Numeric arrays are already in the native sing-box shape.
		return nil
	}

	ports, ranges, err := parsePanelPortSelector(text)
	if err != nil {
		return fmt.Errorf("%s.%s: %w", path, field, err)
	}
	delete(rule, field)
	if len(ports) > 0 {
		rule[field] = ports
	}
	if len(ranges) == 0 {
		return nil
	}

	existing, err := runtimeStringSlice(rule[rangeField])
	if err != nil {
		return fmt.Errorf("%s.%s: %w", path, rangeField, err)
	}
	for _, item := range ranges {
		if !containsString(existing, item) {
			existing = append(existing, item)
		}
	}
	rule[rangeField] = existing
	return nil
}

func parsePanelPortSelector(value string) ([]int, []string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil, nil
	}

	var ports []int
	var ranges []string
	seenPorts := map[int]struct{}{}
	seenRanges := map[string]struct{}{}
	for _, rawToken := range strings.Split(value, ",") {
		token := strings.TrimSpace(rawToken)
		if token == "" {
			continue
		}
		if strings.Contains(token, ":") {
			parts := strings.Split(token, ":")
			if len(parts) != 2 {
				return nil, nil, fmt.Errorf("invalid port range %q", token)
			}
			start, err := parseRuntimePort(parts[0])
			if err != nil {
				return nil, nil, fmt.Errorf("invalid port range %q: %w", token, err)
			}
			end, err := parseRuntimePort(parts[1])
			if err != nil {
				return nil, nil, fmt.Errorf("invalid port range %q: %w", token, err)
			}
			if start > end {
				return nil, nil, fmt.Errorf("invalid port range %q: start exceeds end", token)
			}
			normalized := strconv.Itoa(start) + ":" + strconv.Itoa(end)
			if _, exists := seenRanges[normalized]; !exists {
				seenRanges[normalized] = struct{}{}
				ranges = append(ranges, normalized)
			}
			continue
		}

		port, err := parseRuntimePort(token)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := seenPorts[port]; !exists {
			seenPorts[port] = struct{}{}
			ports = append(ports, port)
		}
	}
	return ports, ranges, nil
}

func parseRuntimePort(value string) (int, error) {
	value = strings.TrimSpace(value)
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port %q", value)
	}
	return port, nil
}

func runtimeStringSlice(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...), nil
	case []any:
		result := make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("must contain only strings")
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("must be an array")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
