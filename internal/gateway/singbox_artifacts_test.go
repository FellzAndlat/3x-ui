package gateway

import "testing"

func TestHasSingBoxGatewayArtifactsRecognizesPartialState(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]any
		want bool
	}{
		{
			name: "empty",
			cfg:  map[string]any{},
			want: false,
		},
		{
			name: "gateway inbound only",
			cfg: map[string]any{
				"inbounds": []any{singBoxGatewayInbound()},
			},
			want: true,
		},
		{
			name: "gateway sniff rule only",
			cfg: map[string]any{
				"route": map[string]any{
					"rules": []any{singBoxGatewaySniffRule()},
				},
			},
			want: true,
		},
		{
			name: "fully configured",
			cfg: map[string]any{
				"inbounds": []any{singBoxGatewayInbound()},
				"route": map[string]any{
					"rules": []any{singBoxGatewaySniffRule()},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasSingBoxGatewayArtifacts(tt.cfg); got != tt.want {
				t.Fatalf("hasSingBoxGatewayArtifacts() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoveSingBoxGatewayConfigRemovesOrphanSniffRule(t *testing.T) {
	cfg := map[string]any{
		"route": map[string]any{
			"rules": []any{singBoxGatewaySniffRule()},
		},
	}

	changed, err := removeSingBoxGatewayConfig(cfg)
	if err != nil {
		t.Fatalf("removeSingBoxGatewayConfig() error = %v", err)
	}
	if !changed {
		t.Fatal("removeSingBoxGatewayConfig() changed = false, want true")
	}
	if hasSingBoxGatewayArtifacts(cfg) {
		t.Fatalf("Gateway artifacts remain after cleanup: %#v", cfg)
	}
}
