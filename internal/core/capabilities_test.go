package core

import "testing"

func TestCapabilitiesFor(t *testing.T) {
	tests := []struct {
		name string
		core Type
		want Capabilities
	}{
		{
			name: "xray",
			core: Xray,
			want: Capabilities{
				ConnectionStats:  true,
				HotInboundReload: true,
				GeoIP:            true,
				GeoSite:          true,
			},
		},
		{
			name: "sing-box",
			core: SingBox,
			want: Capabilities{
				RuleSets:           true,
				OutboundDelayProbe: true,
				ConnectionStats:    true,
				ClashAPI:           true,
				ShadowTLS:          true,
			},
		},
		{name: "unknown", core: Type("unknown"), want: Capabilities{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CapabilitiesFor(tc.core); got != tc.want {
				t.Fatalf("CapabilitiesFor(%q) = %+v, want %+v", tc.core, got, tc.want)
			}
			if got := tc.core.Capabilities(); got != tc.want {
				t.Fatalf("Type.Capabilities(%q) = %+v, want %+v", tc.core, got, tc.want)
			}
		})
	}
}
