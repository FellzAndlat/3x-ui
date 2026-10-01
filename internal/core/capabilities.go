package core

// Capabilities describes optional behavior exposed by a proxy core. Keep this
// separate from Runtime so adding a core-specific feature never expands the
// lifecycle contract or forces unrelated adapters to implement stub methods.
type Capabilities struct {
	RuleSets           bool `json:"ruleSets"`
	OutboundDelayProbe bool `json:"outboundDelayProbe"`
	ConnectionStats    bool `json:"connectionStats"`
	HotInboundReload   bool `json:"hotInboundReload"`
	ClashAPI           bool `json:"clashApi"`
	GeoIP              bool `json:"geoIp"`
	GeoSite            bool `json:"geoSite"`
	ShadowTLS          bool `json:"shadowTls"`
}

// CapabilitiesFor returns the features the panel can safely use for the
// selected core. These are panel capabilities, not an exhaustive list of what
// an upstream binary might support: a flag is true only when 3X-UI has the
// integration needed to use it.
func CapabilitiesFor(t Type) Capabilities {
	switch t {
	case Xray:
		return Capabilities{
			ConnectionStats:  true,
			HotInboundReload: true,
			GeoIP:            true,
			GeoSite:          true,
		}
	case SingBox:
		return Capabilities{
			RuleSets:           true,
			OutboundDelayProbe: true,
			ConnectionStats:    true,
			ClashAPI:           true,
			ShadowTLS:          true,
		}
	default:
		return Capabilities{}
	}
}

// Capabilities returns the panel-supported optional features for this core.
func (t Type) Capabilities() Capabilities { return CapabilitiesFor(t) }
