package service

import (
	"encoding/json"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/amneziawg"
	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/util/common"
)

// validateSingBoxTemplateOutbound mirrors the outbound handling in
// SingBoxService.GetConfig. Xray-shaped templates are shared by both cores, so
// validating them with Xray's loader while sing-box is selected rejects native
// sing-box protocols and misses sing-box-specific translation errors.
func validateSingBoxTemplateOutbound(raw json.RawMessage, managed map[string]any) error {
	if externalvpn.IsAdditionalOutbound(managed) {
		return externalvpn.ValidateAdditionalOutbound(managed)
	}
	if amneziawg.IsAmneziaWGOutbound(raw) {
		var probe struct {
			Tag string `json:"tag"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return common.NewError("xray template config invalid: amneziawg outbound tag unreadable:", err)
		}
		return amneziawg.ValidateAmneziaWGOutbound(probe.Tag, raw)
	}

	protocol, _ := managed["protocol"].(string)
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch protocol {
	case "dns":
		// The sing-box generator translates Xray DNS routing references and does
		// not emit the legacy Xray DNS outbound itself.
		return nil
	case "wireguard":
		_, err := singbox.TranslateXrayWireGuardEndpoint(managed)
		return err
	default:
		_, err := singbox.TranslateXrayOutbound(managed)
		return err
	}
}
