package service

import (
	"encoding/json"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/util/common"
)

func validateInboundRuntimeProtocol(protocol model.Protocol, existing *model.Inbound) error {
	if protocol != model.NaiveProxy && protocol != model.AnyTLS && protocol != model.ShadowTLS {
		return nil
	}
	core, err := (&SettingService{}).GetCoreType()
	if err != nil {
		return err
	}
	if core == CoreTypeSingBox {
		return nil
	}
	// NaïveProxy is not an Xray-managed protocol. Keeping an existing Naïve
	// row while Xray is selected makes it look enabled in the UI while the
	// Xray config silently omits it.
	switch protocol {
	case model.NaiveProxy:
		return common.NewErrorf("NaïveProxy requires sing-box as the selected core")
	case model.AnyTLS:
		return common.NewErrorf("AnyTLS requires sing-box as the selected core")
	case model.ShadowTLS:
		return common.NewErrorf("ShadowTLS requires sing-box as the selected core")
	default:
		return common.NewErrorf("%s requires sing-box as the selected core", protocol)
	}
}

func validateShadowTLSTransport(inbound *model.Inbound) error {
	if model.ShadowTLSTransport(inbound.Settings) == nil {
		return nil
	}
	if !model.SupportsShadowTLSTransport(inbound.Protocol) {
		return common.NewErrorf("ShadowTLS cannot wrap %s", inbound.Protocol)
	}
	if inbound.Protocol == model.Mixed {
		var settings struct {
			UDP bool `json:"udp"`
		}
		_ = json.Unmarshal([]byte(inbound.Settings), &settings)
		if settings.UDP {
			return common.NewError("ShadowTLS only supports TCP; disable Mixed UDP")
		}
	}
	if inbound.Protocol == model.VLESS {
		var settings struct {
			Encryption string `json:"encryption"`
			Decryption string `json:"decryption"`
		}
		_ = json.Unmarshal([]byte(inbound.Settings), &settings)
		if (settings.Encryption != "" && settings.Encryption != "none") || (settings.Decryption != "" && settings.Decryption != "none") {
			return common.NewError("sing-box cannot represent VLESS encryption with ShadowTLS")
		}
	}
	var stream struct {
		Network  string `json:"network"`
		Security string `json:"security"`
	}
	_ = json.Unmarshal([]byte(inbound.StreamSettings), &stream)
	if (stream.Network != "" && stream.Network != "tcp") || (stream.Security != "" && !strings.EqualFold(stream.Security, "none")) {
		return common.NewError("ShadowTLS requires RAW TCP without inner TLS or REALITY")
	}
	return validateInboundRuntimeProtocol(model.ShadowTLS, nil)
}

func isXrayManagedProtocol(protocol model.Protocol) bool {
	return protocol != model.VKTurnProxy &&
		protocol != model.NaiveProxy &&
		protocol != model.AnyTLS &&
		protocol != model.ShadowTLS &&
		protocol != model.MTProto &&
		protocol != model.AmneziaWG &&
		protocol != model.TUIC &&
		protocol != model.Pingtunnel &&
		protocol != model.TrustTunnel &&
		protocol != model.Mieru &&
		protocol != model.Sudoku
}

func isVKTurnProxyProtocol(protocol model.Protocol) bool {
	return protocol == model.VKTurnProxy
}
