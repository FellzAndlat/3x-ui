package externalvpn

import "math"

// cumulativeTrafficDelta converts a monotonically increasing uint64 counter
// into the signed representation used by the panel without allowing a large
// valid delta to wrap negative. Counter resets are treated as a fresh baseline.
func cumulativeTrafficDelta(current, previous uint64) int64 {
	if current < previous {
		previous = 0
	}
	delta := current - previous
	if delta > uint64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(delta)
}
