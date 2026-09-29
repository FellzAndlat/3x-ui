package mtproto

import "testing"

func TestMonotonicCounterDelta(t *testing.T) {
	tests := []struct {
		name     string
		current  int64
		previous int64
		want     int64
	}{
		{name: "normal growth", current: 150, previous: 100, want: 50},
		{name: "unchanged", current: 100, previous: 100, want: 0},
		{name: "counter reset", current: 20, previous: 100, want: 20},
		{name: "reset to zero", current: 0, previous: 100, want: 0},
		{name: "invalid negative current", current: -1, previous: 100, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := monotonicCounterDelta(tt.current, tt.previous); got != tt.want {
				t.Fatalf("monotonicCounterDelta(%d, %d) = %d, want %d", tt.current, tt.previous, got, tt.want)
			}
		})
	}
}
