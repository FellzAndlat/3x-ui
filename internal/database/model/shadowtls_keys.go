package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const ShadowTLSInnerKeyField = "innerKey"

// NewShadowTLSInnerKey creates the 128-bit server key required by the
// Shadowsocks 2022 inner protocol. Store it with the inbound settings.
func NewShadowTLSInnerKey() (string, error) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

func ValidShadowTLSInnerKey(key string) bool {
	decoded, err := base64.StdEncoding.DecodeString(key)
	return err == nil && len(decoded) == 16
}

// ShadowTLSClientKey is stable for a client's identity and password while
// keeping the Shadowsocks user key independent of the outer ShadowTLS secret.
func ShadowTLSClientKey(email, password string) string {
	hash := sha256.Sum256([]byte("3x-ui shadowtls ss2022 user\x00" + strings.ToLower(strings.TrimSpace(email)) + "\x00" + password))
	return base64.StdEncoding.EncodeToString(hash[:16])
}

// EnsureShadowTLSInnerKey preserves an existing key or adds a random one.
func EnsureShadowTLSInnerKey(settings, previous string) (string, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(settings), &fields); err != nil {
		return "", fmt.Errorf("decode ShadowTLS settings: %w", err)
	}
	var current string
	_ = json.Unmarshal(fields[ShadowTLSInnerKeyField], &current)
	if !ValidShadowTLSInnerKey(current) {
		old := map[string]json.RawMessage{}
		_ = json.Unmarshal([]byte(previous), &old)
		_ = json.Unmarshal(old[ShadowTLSInnerKeyField], &current)
		if !ValidShadowTLSInnerKey(current) {
			var err error
			current, err = NewShadowTLSInnerKey()
			if err != nil {
				return "", err
			}
		}
		encoded, _ := json.Marshal(current)
		fields[ShadowTLSInnerKeyField] = encoded
	}
	encoded, err := json.Marshal(fields)
	return string(encoded), err
}
