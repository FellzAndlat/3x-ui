package externalvpn

import (
	"math"
	"testing"
)

func TestCumulativeTrafficDelta(t *testing.T) {
	tests := []struct {
		name              string
		current, previous uint64
		want              int64
	}{
		{name: "normal", current: 150, previous: 100, want: 50},
		{name: "counter reset", current: 25, previous: 100, want: 25},
		{name: "saturates instead of wrapping", current: math.MaxUint64, previous: 0, want: math.MaxInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cumulativeTrafficDelta(tt.current, tt.previous); got != tt.want {
				t.Fatalf("cumulativeTrafficDelta(%d, %d) = %d, want %d", tt.current, tt.previous, got, tt.want)
			}
		})
	}
}
