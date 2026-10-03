package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
)

func subscriptionCompatibilityCategory(issue string) string {
	lower := strings.ToLower(issue)
	switch {
	case strings.Contains(lower, "unsupported vless encryption"):
		return "VLESS encryption"
	case strings.Contains(lower, "unsupported xray transport \"xhttp\""):
		return "XHTTP transport"
	case strings.Contains(lower, "grpc authority"):
		return "gRPC authority"
	case strings.Contains(lower, "grpc user_agent"):
		return "gRPC user_agent"
	case strings.Contains(lower, "grpc multimode"):
		return "gRPC multiMode"
	case strings.Contains(lower, "grpc initial_windows_size"):
		return "gRPC window settings"
	case strings.Contains(lower, "xray quic transport"):
		return "Xray QUIC transport"
	default:
		return "other"
	}
}

func logSubscriptionCompatibilitySummary(label, coreName string, issues []string) {
	if len(issues) == 0 {
		return
	}
	counts := make(map[string]int)
	for _, issue := range issues {
		counts[subscriptionCompatibilityCategory(issue)]++
	}
	categories := make([]string, 0, len(counts))
	for category := range counts {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	parts := make([]string, 0, len(categories))
	for _, category := range categories {
		parts = append(parts, fmt.Sprintf("%s=%d", category, counts[category]))
	}
	logger.Warningf(
		"%s: %d outbound(s) are incompatible with current %s runtime and were skipped (%s); profiles remain stored for core switching/upgrades",
		label, len(issues), coreName, strings.Join(parts, ", "),
	)
}

// filterSubscriptionOutboundsWithIssues validates subscription members against
// the active core. Runtime-only core incompatibilities are logged and filtered
// without being returned as subscription refresh errors: fetching/parsing the
// subscription still succeeded and all source profiles remain stored. The
// returned issues are reserved for malformed/invalid source entries rather than
// expected feature gaps between Xray and sing-box.
func filterSubscriptionOutboundsWithIssues(label string, members []any) ([]any, []string, string) {
	core, _ := (&SettingService{}).GetCoreType()
	if core != CoreTypeSingBox {
		kept, compatibilityIssues := filterOutboundsRejectedByCore(label, members)
		logSubscriptionCompatibilitySummary(label, "xray", compatibilityIssues)
		return kept, nil, "xray"
	}

	kept := make([]any, 0, len(members))
	statusIssues := make([]string, 0)
	compatibilityIssues := make([]string, 0)
	for index, raw := range members {
		ob, ok := raw.(map[string]any)
		if !ok {
			statusIssues = append(statusIssues, fmt.Sprintf("profile %d: invalid outbound JSON object", index+1))
			continue
		}
		tag, _ := ob["tag"].(string)
		if strings.TrimSpace(tag) == "" {
			tag = fmt.Sprintf("profile %d", index+1)
		}
		protocol, _ := ob["protocol"].(string)
		var err error
		if externalvpn.IsAdditionalOutbound(ob) {
			if err := externalvpn.ValidateAdditionalOutbound(ob); err != nil {
				statusIssues = append(statusIssues, fmt.Sprintf("%s: %v", tag, err))
			} else {
				kept = append(kept, raw)
			}
			continue
		}
		switch strings.ToLower(strings.TrimSpace(protocol)) {
		case "loopback":
			err = fmt.Errorf("Xray loopback members cannot be translated to sing-box")
		case "amneziawg":
			// The runtime and probe builders supply a SOCKS bridge.
		case "wireguard":
			_, err = singbox.TranslateXrayWireGuardEndpoint(ob)
		default:
			_, err = singbox.TranslateXrayOutbound(ob)
		}
		if err != nil {
			compatibilityIssues = append(compatibilityIssues, fmt.Sprintf("%s: %v", tag, err))
			continue
		}
		kept = append(kept, raw)
	}
	logSubscriptionCompatibilitySummary(label, "sing-box", compatibilityIssues)
	return kept, statusIssues, "sing-box"
}

// Unsupported subscription members must not prevent the active core starting.
// Keep the original stored subscription so switching cores can restore them.
func filterSubscriptionOutbounds(label string, members []any) []any {
	kept, _, _ := filterSubscriptionOutboundsWithIssues(label, members)
	return kept
}
