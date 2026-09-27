package model

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ShadowTLS can inject a TCP connection into these sing-box inbounds.
func SupportsShadowTLSTransport(protocol Protocol) bool {
	switch protocol {
	case VLESS, VMESS, Trojan, Shadowsocks, HTTP, Mixed, AnyTLS:
		return true
	}
	return false
}

func ShadowTLSTransport(settings string) map[string]any {
	var data struct {
		ShadowTLS map[string]any `json:"shadowTls"`
	}
	if json.Unmarshal([]byte(settings), &data) != nil || data.ShadowTLS["enabled"] != true {
		return nil
	}
	return data.ShadowTLS
}

// Keep the outer credential stable across updates and use the inner protocol
// for individual client authentication and accounting.
func EnsureShadowTLSTransportPassword(settings, previous string) (string, error) {
	transport := ShadowTLSTransport(settings)
	if transport == nil {
		return settings, nil
	}
	if value, _ := transport["password"].(string); value == "" {
		old := ShadowTLSTransport(previous)
		value, _ = old["password"].(string)
		if value == "" {
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return "", err
			}
			value = base64.StdEncoding.EncodeToString(key)
		}
		transport["password"] = value
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(settings), &data); err != nil {
		return "", fmt.Errorf("decode ShadowTLS transport settings: %w", err)
	}
	data["shadowTls"] = transport
	encoded, err := json.Marshal(data)
	return string(encoded), err
}
