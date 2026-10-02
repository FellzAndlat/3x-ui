//go:build !linux

package mtproto

// killStrayTelemtSidecars is a no-op off Linux. On Windows the kill-on-exit
// job object already terminates Telemt together with the panel (see
// attachChildLifetime), so sidecar orphans do not arise there; other platforms
// are not supported deployment targets for managed MTProto Telemt sidecars.
func killStrayTelemtSidecars(_ string) int { return 0 }
