package job

import "math"

// unsignedTrafficDelta converts cumulative-counter deltas to the signed traffic
// representation used by Xray models. Saturating avoids uint64 values wrapping
// negative and being silently discarded by the accounting pipeline.
func unsignedTrafficDelta(delta uint64) int64 {
	if delta > uint64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(delta)
}
