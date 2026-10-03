package service

import (
	"fmt"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"strings"
)

// Unsupported subscription members must not prevent the active core starting.
// Keep the original stored subscription so switching cores can restore them.
func filterSubscriptionOutbounds(label string, members []any) []any {
	core, _ := (&SettingService{}).GetCoreType()
	if core != CoreTypeSingBox {
		kept, _ := filterOutboundsRejectedByCore(label, members)
		return kept
	}
	kept := make([]any, 0, len(members))
	for _, raw := range members {
		ob, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		protocol, _ := ob["protocol"].(string)
		var err error
		switch strings.ToLower(protocol) {
		case "loopback":
			err = fmt.Errorf("Xray loopback members cannot be translated to sing-box")
		case "amneziawg": // The runtime and probe builders supply a SOCKS bridge.
		case "wireguard":
			_, err = singbox.TranslateXrayWireGuardEndpoint(ob)
		default:
			_, err = singbox.TranslateXrayOutbound(ob)
		}
		if err != nil {
			logger.Warningf("%s: ignoring incompatible sing-box outbound %v: %v", label, ob["tag"], err)
			continue
		}
		kept = append(kept, raw)
	}
	return kept
}
