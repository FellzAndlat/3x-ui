package singbox

import (
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
)

func TestInboundTLSECHServerKeysTranslateToSingBoxPEM(t *testing.T) {
	// Xray echServerKeys is a base64 encoding of the same length-prefixed key
	// set that sing-box expects inside an ECH KEYS PEM block.
	rawKeySet := []byte{0, 1, 0xaa, 0, 2, 0xbb, 0xcc}
	out := map[string]any{"tag": "ech-in"}
	err := translateStream(out, "vless", map[string]any{
		"security": "tls",
		"tlsSettings": map[string]any{
			"echServerKeys": base64.StdEncoding.EncodeToString(rawKeySet),
		},
	}, true)
	if err != nil {
		t.Fatalf("translate inbound ECH: %v", err)
	}

	tlsOptions := out["tls"].(map[string]any)
	ech := tlsOptions["ech"].(map[string]any)
	if enabled, _ := ech["enabled"].(bool); !enabled {
		t.Fatalf("ECH is not enabled: %#v", ech)
	}
	lines, ok := ech["key"].([]string)
	if !ok || len(lines) == 0 {
		t.Fatalf("ECH key is not a PEM line array: %#v", ech["key"])
	}
	block, rest := pem.Decode([]byte(strings.Join(lines, "\n") + "\n"))
	if block == nil || block.Type != "ECH KEYS" || len(strings.TrimSpace(string(rest))) != 0 {
		t.Fatalf("translated ECH key is not a clean ECH KEYS PEM block: %q", strings.Join(lines, "\n"))
	}
	if string(block.Bytes) != string(rawKeySet) {
		t.Fatalf("ECH payload changed: got %x want %x", block.Bytes, rawKeySet)
	}
}

func TestInboundTLSECHServerKeysRejectInvalidBase64(t *testing.T) {
	out := map[string]any{"tag": "ech-in"}
	err := translateStream(out, "vless", map[string]any{
		"security": "tls",
		"tlsSettings": map[string]any{"echServerKeys": "not base64 !!!"},
	}, true)
	if err == nil || !strings.Contains(err.Error(), "echServerKeys") {
		t.Fatalf("error = %v, want an echServerKeys diagnostic", err)
	}
}

func TestInboundRealityToleratesXrayMLDSASeed(t *testing.T) {
	out := map[string]any{"tag": "reality-in"}
	err := translateStream(out, "vless", map[string]any{
		"security": "reality",
		"realitySettings": map[string]any{
			"target":      "example.com:443",
			"privateKey":  "private",
			"shortIds":    []any{"0123456789abcdef"},
			"mldsa65Seed": "xray-only-seed",
		},
	}, true)
	if err != nil {
		t.Fatalf("Xray-only inbound mldsa65Seed must not block sing-box translation: %v", err)
	}
	reality := out["tls"].(map[string]any)["reality"].(map[string]any)
	if _, leaked := reality["mldsa65Seed"]; leaked {
		t.Fatalf("Xray-only ML-DSA seed leaked into sing-box REALITY: %#v", reality)
	}
	if _, leaked := reality["mldsa65_seed"]; leaked {
		t.Fatalf("invented ML-DSA field leaked into sing-box REALITY: %#v", reality)
	}
}

func TestOutboundRealityStillRejectsUnsupportedMLDSA(t *testing.T) {
	out := map[string]any{"tag": "reality-out"}
	err := translateStream(out, "vless", map[string]any{
		"security": "reality",
		"realitySettings": map[string]any{
			"publicKey":    "public",
			"serverName":   "example.com",
			"fingerprint":  "chrome",
			"mldsa65Verify": "verify-key",
		},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "mldsa65Verify") {
		t.Fatalf("error = %v, want unsupported outbound mldsa65Verify rejection", err)
	}
}
