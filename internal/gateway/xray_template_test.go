package gateway

import (
	"strings"
	"testing"
)

func TestDecodeXrayTemplateRejectsNullRoot(t *testing.T) {
	cfg, err := decodeXrayTemplate("null")
	if err == nil {
		t.Fatal("decodeXrayTemplate() accepted null top-level config")
	}
	if cfg != nil {
		t.Fatalf("decodeXrayTemplate() cfg = %#v, want nil", cfg)
	}
	if !strings.Contains(err.Error(), "top-level JSON value must be an object") {
		t.Fatalf("decodeXrayTemplate() error = %v, want object validation error", err)
	}
}

func TestDecodeXrayTemplateAcceptsObject(t *testing.T) {
	cfg, err := decodeXrayTemplate(`{"log":{"loglevel":"warning"},"inbounds":[]}`)
	if err != nil {
		t.Fatalf("decodeXrayTemplate() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("decodeXrayTemplate() returned nil config")
	}
	if _, ok := cfg["log"].(map[string]any); !ok {
		t.Fatalf("decoded log section type = %T, want map[string]any", cfg["log"])
	}
}
