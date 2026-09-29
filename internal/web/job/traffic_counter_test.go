package job

import (
	"math"
	"testing"
)

func TestUnsignedTrafficDelta(t *testing.T) {
	if got := unsignedTrafficDelta(42); got != 42 {
		t.Fatalf("unsignedTrafficDelta(42) = %d, want 42", got)
	}
	if got := unsignedTrafficDelta(uint64(math.MaxInt64) + 1); got != math.MaxInt64 {
		t.Fatalf("overflow delta = %d, want %d", got, int64(math.MaxInt64))
	}
}
