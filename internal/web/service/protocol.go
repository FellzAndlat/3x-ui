package service

import (
	"encoding/json"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/util/common"
)

// validateInboundRuntimeTarget validates the protocol against the core that
// will actually host the inbound. Local inbounds preserve the historical rule
// that only the sing-box-only protocols are gated here. Node inbounds are
// checked against the node's persisted core so an Xray master can manage a
// sing-box node (and vice versa) without validating against the wrong engine.
func validateInboundRuntimeTarget(inbound *model.Inbound) error {
	if inbound == nil {
		return nil
	}
	core, err := (&InboundService{}).coreTypeForInbound(inbound)
	if err != nil {
		return err
	}
	if inbound.NodeID != nil {
		if coreSupportsInboundProtocol(core, inbound.Protocol) {
			return nil
		}
		return common.NewErrorf("%s is not supported by %s on the selected node", inbound.Protocol, core)
	}
	if inbound.Protocol != model.Snell && inbound.Protocol != model.NaiveProxy && inbound.Protocol != model.AnyTLS && inbound.Protocol != model.ShadowTLS {
		return nil
	}
	if core == CoreTypeSingBox {
		return nil
	}
	switch inbound.Protocol {
	case model.NaiveProxy:
		return common.NewErrorf("NaïveProxy requires sing-box as the selected core")
	case model.AnyTLS:
		return common.NewErrorf("AnyTLS requires sing-box as the selected core")
	case model.ShadowTLS:
		return common.NewErrorf("ShadowTLS requires sing-box as the selected core")
	default:
		return common.NewErrorf("%s requires sing-box as the selected core", inbound.Protocol)
	}
}

func validateInboundRuntimeProtocol(protocol model.Protocol, existing *model.Inbound) error {
	// AddInbound validates through validateShadowTLSTransport while it still has
	// the complete incoming row, including NodeID. On update, NodeID in the wire
	// payload is intentionally not trusted; validate against the stored host row.
	if existing == nil {
		return nil
	}
	candidate := *existing
	candidate.Protocol = protocol
	return validateInboundRuntimeTarget(&candidate)
}

// shadowTLSTarget resolves the runtime that will host an inbound carrying a
// ShadowTLS transport wrapper. Update payloads cannot be trusted for NodeID, so
// recover the stored assignment before checking the target core.
func shadowTLSTarget(inbound *model.Inbound) (*model.Inbound, error) {
	if inbound == nil || inbound.Id == 0 {
		return inbound, nil
	}
	stored, err := (&InboundService{}).GetInbound(inbound.Id)
	if err != nil {
		return nil, err
	}
	candidate := *stored
	candidate.Protocol = inbound.Protocol
	return &candidate, nil
}

func validateShadowTLSTransport(inbound *model.Inbound) error {
	if inbound == nil {
		return nil
	}
	// AddInbound resets Id to zero before this call, so this is the one early
	// validation point that still has the requested node assignment. UpdateInbound
	// validates ordinary protocol compatibility later against the stored row.
	if inbound.Id == 0 {
		if err := validateInboundRuntimeTarget(inbound); err != nil {
			return err
		}
	}
	if model.ShadowTLSTransport(inbound.Settings) == nil {
		return nil
	}

	// ShadowTLS as a wrapper is sing-box-only even when its inner protocol
	// (VLESS/VMess/Trojan/Shadowsocks/etc.) is also valid in Xray. Resolve the
	// actual target separately or an Xray master/node can accept a row that its
	// generated runtime config can never represent.
	target, err := shadowTLSTarget(inbound)
	if err != nil {
		return err
	}
	if err := validateInboundRuntimeTarget(target); err != nil {
		return err
	}
	core, err := (&InboundService{}).coreTypeForInbound(target)
	if err != nil {
		return err
	}
	if core != CoreTypeSingBox {
		if target != nil && target.NodeID != nil {
			return common.NewError("ShadowTLS transport requires sing-box on the selected node")
		}
		return common.NewError("ShadowTLS transport requires sing-box as the selected core")
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
	return nil
}

func isXrayManagedProtocol(protocol model.Protocol) bool {
	return protocol != model.VKTurnProxy &&
		protocol != model.Snell && protocol != model.NaiveProxy &&
		protocol != model.AnyTLS &&
		protocol != model.ShadowTLS &&
		protocol != model.MTProto &&
		protocol != model.AmneziaWG &&
		protocol != model.TUIC &&
		protocol != model.Pingtunnel &&
		protocol != model.TrustTunnel && protocol != model.FPTN && protocol != model.OpenFlux &&
		protocol != model.Mieru &&
		protocol != model.Sudoku
}

func isVKTurnProxyProtocol(protocol model.Protocol) bool {
	return protocol == model.VKTurnProxy
}
