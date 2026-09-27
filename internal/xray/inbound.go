package xray

import (
	"bytes"
	"encoding/json"

	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
)

// InboundConfig represents an Xray inbound configuration.
// It defines how Xray accepts incoming connections including protocol, port, and settings.
type InboundConfig struct {
	Listen         json_util.RawMessage `json:"listen"` // listen cannot be an empty string
	Port           int                  `json:"port"`
	Protocol       string               `json:"protocol"`
	Settings       json_util.RawMessage `json:"settings"`
	StreamSettings json_util.RawMessage `json:"streamSettings,omitempty"`
	Tag            string               `json:"tag"`
	Sniffing       json_util.RawMessage `json:"sniffing,omitempty"`
}

// MarshalJSON strips panel-only VLESS fields from the wire config sent to
// xray-core. VLESS encryption stores the client-side "encryption" value next to
// the server-side "decryption" value so subscriptions can be generated from the
// same inbound row, but xray-core's inbound only consumes decryption.
//
// xray-core also treats an explicitly present empty fallbacks array as
// configured. With VLESS encryption enabled (decryption != "none"), that makes
// Build reject the inbound even though there are no actual fallbacks. Omit only
// the empty array in that mode; non-empty fallbacks are intentionally preserved
// so the core reports the invalid combination instead of silently discarding an
// operator configuration.
func (c InboundConfig) MarshalJSON() ([]byte, error) {
	type inboundConfigAlias InboundConfig
	wire := inboundConfigAlias(c)
	if c.Protocol == "vless" {
		wire.Settings = sanitizeVlessInboundSettings(c.Settings)
	}
	return json.Marshal(wire)
}

func sanitizeVlessInboundSettings(settings json_util.RawMessage) json_util.RawMessage {
	if len(settings) == 0 {
		return settings
	}

	var parsed map[string]any
	if err := json.Unmarshal(settings, &parsed); err != nil {
		return settings
	}

	changed := false
	if _, ok := parsed["encryption"]; ok {
		delete(parsed, "encryption")
		changed = true
	}

	decryption, _ := parsed["decryption"].(string)
	if decryption != "" && decryption != "none" {
		if fallbacks, ok := parsed["fallbacks"].([]any); ok && len(fallbacks) == 0 {
			delete(parsed, "fallbacks")
			changed = true
		}
	}

	if !changed {
		return settings
	}
	out, err := json.Marshal(parsed)
	if err != nil {
		return settings
	}
	return json_util.RawMessage(out)
}

// Equals compares two InboundConfig instances for deep equality.
func (c *InboundConfig) Equals(other *InboundConfig) bool {
	if !bytes.Equal(c.Listen, other.Listen) {
		return false
	}
	if c.Port != other.Port {
		return false
	}
	if c.Protocol != other.Protocol {
		return false
	}
	if !bytes.Equal(c.Settings, other.Settings) {
		return false
	}
	if !bytes.Equal(c.StreamSettings, other.StreamSettings) {
		return false
	}
	if c.Tag != other.Tag {
		return false
	}
	if !bytes.Equal(c.Sniffing, other.Sniffing) {
		return false
	}
	return true
}
