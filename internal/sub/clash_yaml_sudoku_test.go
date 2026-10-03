package sub

import "testing"

func TestNormalizeSudokuMihomoProxy(t *testing.T) {
	proxy := map[string]any{
		"type":  "sudoku",
		"aead":  "chacha20-poly1305",
		"ascii": "entropy",
		"tls":   true,
		"http-mask": map[string]any{
			"pathRoot": "/mask",
			"mode":     "auto",
		},
	}

	normalized, ok := normalizeClashCompatibility(proxy).(map[string]any)
	if !ok {
		t.Fatal("normalized proxy is not a map")
	}
	if _, exists := normalized["aead"]; exists {
		t.Fatal("legacy aead field was not removed")
	}
	if normalized["aead-method"] != "chacha20-poly1305" {
		t.Fatalf("aead-method = %#v", normalized["aead-method"])
	}
	if _, exists := normalized["ascii"]; exists {
		t.Fatal("legacy ascii field was not removed")
	}
	if normalized["table-type"] != "prefer_entropy" {
		t.Fatalf("table-type = %#v", normalized["table-type"])
	}
	if _, exists := normalized["http-mask"]; exists {
		t.Fatal("legacy http-mask field was not removed")
	}
	if _, exists := normalized["tls"]; exists {
		t.Fatal("top-level tls field was not removed")
	}
	httpMask, ok := normalized["httpmask"].(map[string]any)
	if !ok {
		t.Fatalf("httpmask = %#v", normalized["httpmask"])
	}
	if _, exists := httpMask["pathRoot"]; exists {
		t.Fatal("legacy pathRoot field was not removed")
	}
	if httpMask["path-root"] != "/mask" {
		t.Fatalf("path-root = %#v", httpMask["path-root"])
	}
	if httpMask["tls"] != true {
		t.Fatalf("nested tls = %#v", httpMask["tls"])
	}
}

func TestNormalizeClashCompatibilityLeavesOtherProtocolsAlone(t *testing.T) {
	proxy := map[string]any{
		"type":  "vless",
		"aead":  "keep-me",
		"ascii": "keep-me-too",
	}
	normalized := normalizeClashCompatibility(proxy).(map[string]any)
	if normalized["aead"] != "keep-me" || normalized["ascii"] != "keep-me-too" {
		t.Fatalf("non-Sudoku proxy was changed: %#v", normalized)
	}
}
