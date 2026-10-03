package service

import (
	"fmt"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
)

// filterSubscriptionOutboundsWithIssues validates subscription members against
// the active core. It returns the runtime-compatible members together with a
// per-profile explanation for every member the active core cannot represent.
// Stored subscription data is intentionally not mutated by this helper so a
// profile unsupported by one core can become usable after switching cores.
func filterSubscriptionOutboundsWithIssues(label string, members []any) ([]any, []string, string) {
	core, _ := (&SettingService{}).GetCoreType()
	if core != CoreTypeSingBox {
		kept, issues := filterOutboundsRejectedByCore(label, members)
		return kept, issues, "xray"
	}

	kept := make([]any, 0, len(members))
	issues := make([]string, 0)
	for index, raw := range members {
		ob, ok := raw.(map[string]any)
		if !ok {
			issues = append(issues, fmt.Sprintf("profile %d: invalid outbound JSON object", index+1))
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
				issues = append(issues, fmt.Sprintf("%s: %v", tag, err))
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
			issue := fmt.Sprintf("%s: %v", tag, err)
			logger.Warningf("%s: incompatible sing-box outbound %q: %v", label, tag, err)
			issues = append(issues, issue)
			continue
		}
		kept = append(kept, raw)
	}
	return kept, issues, "sing-box"
}

// Unsupported subscription members must not prevent the active core starting.
// Keep the original stored subscription so switching cores can restore them.
func filterSubscriptionOutbounds(label string, members []any) []any {
	kept, _, _ := filterSubscriptionOutboundsWithIssues(label, members)
	return kept
}
