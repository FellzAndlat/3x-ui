package link

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
)

func parseAdditionalVPN(link string, protocol string) (*ParseResult, error) {
	var settings map[string]any
	var name string
	if protocol == "fptn" {
		token, err := externalvpn.DecodeFPTNToken(link)
		if err != nil {
			return nil, err
		}
		name = token.Name
		settings = map[string]any{"token": link, "mtu": 1400, "sni": "www.bing.com", "bypass": "sni-spoofing"}
	} else {
		cfg, err := externalvpn.DecodeOpenFluxLink(link)
		if err != nil {
			return nil, err
		}
		name = cfg.Name
		settings = map[string]any{"secret": cfg.Secret, "context": cfg.Context, "transports": cfg.Transports}
	}
	if name == "" {
		name = protocol + "-out"
	}
	outbound := Outbound{"protocol": protocol, "tag": name, "settings": settings}
	if err := externalvpn.ValidateAdditionalOutbound(map[string]any(outbound)); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(settings)
	sum := sha256.Sum256(raw)
	return &ParseResult{Outbound: outbound, Identity: protocol + "|" + hex.EncodeToString(sum[:])}, nil
}
