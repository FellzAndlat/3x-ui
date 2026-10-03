package singbox

import (
	"strings"
	"testing"
)

func TestNormalizeOutboundsForRuntimeRejectsSelfDetour(t *testing.T) {
	_, err := normalizeOutboundsForRuntime([]map[string]any{{
		"type":   "direct",
		"tag":    "loop",
		"detour": "loop",
	}})
	if err == nil || !strings.Contains(err.Error(), "cannot detour to itself") {
		t.Fatalf("expected self detour to be rejected, got %v", err)
	}
}

func TestNormalizeOutboundsForRuntimeRejectsDetourCycle(t *testing.T) {
	_, err := normalizeOutboundsForRuntime([]map[string]any{
		{"type": "direct", "tag": "a", "detour": "b"},
		{"type": "direct", "tag": "b", "detour": "c"},
		{"type": "direct", "tag": "c", "detour": "a"},
	})
	if err == nil || !strings.Contains(err.Error(), "detour cycle") {
		t.Fatalf("expected detour cycle to be rejected, got %v", err)
	}
}

func TestNormalizeOutboundsForRuntimeAllowsAcyclicDetourChain(t *testing.T) {
	_, err := normalizeOutboundsForRuntime([]map[string]any{
		{"type": "direct", "tag": "entry", "detour": "middle"},
		{"type": "direct", "tag": "middle", "detour": "exit"},
		{"type": "direct", "tag": "exit"},
	})
	if err != nil {
		t.Fatalf("acyclic detour chain should be accepted, got %v", err)
	}
}

func TestNormalizeOutboundsForRuntimeLeavesEndpointDetourForFullConfigValidation(t *testing.T) {
	_, err := normalizeOutboundsForRuntime([]map[string]any{{
		"type":   "direct",
		"tag":    "entry",
		"detour": "wireguard-endpoint",
	}})
	if err != nil {
		t.Fatalf("endpoint detour must not be rejected by outbound-only validation, got %v", err)
	}
}
