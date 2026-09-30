package eventbus

import (
	"strings"
	"time"
)

// EventType identifies the kind of event flowing through the bus.
type EventType string

const (
	// Outbound health (observatory-driven)
	EventOutboundDown EventType = "outbound.down"
	EventOutboundUp   EventType = "outbound.up"

	// Xray core (local)
	EventXrayCrash EventType = "xray.crash"

	// Node health (heartbeat-driven)
	EventNodeDown EventType = "node.down"
	EventNodeUp   EventType = "node.up"

	// System health
	EventCPUHigh    EventType = "cpu.high"
	EventMemoryHigh EventType = "memory.high"

	// Security
	EventLoginAttempt EventType = "login.attempt"
)

// Event is the unit of information flowing through the bus.
type Event struct {
	Type      EventType
	Source    string    // outbound tag, node name, client email, IP, etc.
	Data      any       // event-specific payload, may be nil
	Timestamp time.Time // when the event was detected
}

// OutboundHealthData carries observatory details for outbound events.
type OutboundHealthData struct {
	Delay int64  // last measured delay in ms, 0 if unknown
	Error string // last error if probe failed, empty if up
}

// NodeHealthData carries heartbeat details for node events.
// CoreType/RunningCore/CoreState/CoreError are the core-agnostic view used by
// new consumers. The Xray fields remain for backwards compatibility with
// existing notifiers and integrations.
type NodeHealthData struct {
	NodeId       int
	LatencyMs    int
	CpuPct       float64
	MemPct       float64
	CoreType     string
	RunningCore  string
	CoreState    string
	CoreError    string
	XrayState    string // "running", "stopped", etc.
	XrayError    string
	SingBoxState string
	SingBoxError string
}

// EffectiveCore returns the core that is actually running when known, falling
// back to the node's configured core. This keeps old nodes useful while newer
// nodes can report a runtime that differs from configuration during recovery.
func (d *NodeHealthData) EffectiveCore() string {
	if d == nil {
		return ""
	}
	if running := strings.TrimSpace(d.RunningCore); running != "" && !strings.EqualFold(running, "none") {
		return running
	}
	return strings.TrimSpace(d.CoreType)
}

// EffectiveCoreState returns the state for the selected/effective runtime.
func (d *NodeHealthData) EffectiveCoreState() string {
	if d == nil {
		return ""
	}
	if state := strings.TrimSpace(d.CoreState); state != "" {
		return state
	}
	switch strings.ToLower(strings.ReplaceAll(d.EffectiveCore(), "-", "")) {
	case "singbox":
		return strings.TrimSpace(d.SingBoxState)
	case "xray":
		return strings.TrimSpace(d.XrayState)
	}
	if state := strings.TrimSpace(d.SingBoxState); state != "" && strings.TrimSpace(d.XrayState) == "" {
		return state
	}
	return strings.TrimSpace(d.XrayState)
}

// EffectiveCoreError returns the error emitted by the selected/effective core.
func (d *NodeHealthData) EffectiveCoreError() string {
	if d == nil {
		return ""
	}
	if coreErr := strings.TrimSpace(d.CoreError); coreErr != "" {
		return coreErr
	}
	switch strings.ToLower(strings.ReplaceAll(d.EffectiveCore(), "-", "")) {
	case "singbox":
		return strings.TrimSpace(d.SingBoxError)
	case "xray":
		return strings.TrimSpace(d.XrayError)
	}
	if coreErr := strings.TrimSpace(d.SingBoxError); coreErr != "" && strings.TrimSpace(d.XrayError) == "" {
		return coreErr
	}
	return strings.TrimSpace(d.XrayError)
}

// LoginEventData carries login attempt details.
type LoginEventData struct {
	Username string
	IP       string
	Time     string
	Status   string // "success" or "fail"
	Reason   string
}

// SystemMetricData carries raw system metric values for threshold-based events.
type SystemMetricData struct {
	Percent   float64 // current usage percentage
	Threshold int     // configured threshold
}
