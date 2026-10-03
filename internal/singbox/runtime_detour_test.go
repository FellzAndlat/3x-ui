package singbox

import (
	"strings"
	"testing"
)

func TestNormalizeOutboundsForRuntimeRejectsGroupDependencyCycles(t *testing.T) {
	for _, groupType := range []string{"selector", "urltest"} {
		t.Run(groupType, func(t *testing.T) {
			_, err := normalizeOutboundsForRuntime([]map[string]any{
				{"type": groupType, "tag": "group", "outbounds": []string{"proxy"}},
				{"type": "socks", "tag": "proxy", "detour": "group"},
			})
			if err == nil || !strings.Contains(err.Error(), "cycle") {
				t.Fatalf("expected mixed group/detour cycle to be rejected, got %v", err)
			}
		})
	}
}

func TestNormalizeOutboundsForRuntimeAllowsSharedGroupMembers(t *testing.T) {
	_, err := normalizeOutboundsForRuntime([]map[string]any{
		{"type": "selector", "tag": "outer", "outbounds": []string{"inner", "proxy"}},
		{"type": "urltest", "tag": "inner", "outbounds": []string{"proxy", "endpoint"}},
		{"type": "socks", "tag": "proxy", "detour": "endpoint"},
	})
	if err != nil {
		t.Fatalf("acyclic groups and endpoint references should be accepted, got %v", err)
	}
}

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
