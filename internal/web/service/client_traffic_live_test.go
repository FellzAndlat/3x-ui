package service

import (
	"math"
	"testing"
)

func TestNormalizeClientTrafficDelta(t *testing.T) {
	tests := []struct {
		name      string
		delta     int64
		elapsedMs int64
		want      int64
	}{
		{
			name:      "nominal five second sample",
			delta:     25_000,
			elapsedMs: 5_000,
			want:      25_000,
		},
		{
			name:      "late ten second sample",
			delta:     50_000,
			elapsedMs: 10_000,
			want:      25_000,
		},
		{
			name:      "zero delta",
			delta:     0,
			elapsedMs: 10_000,
			want:      0,
		},
		{
			name:      "invalid elapsed falls back to raw delta",
			delta:     25_000,
			elapsedMs: 0,
			want:      25_000,
		},
		{
			name:      "sub-byte normalized delta is inactive",
			delta:     1,
			elapsedMs: 10_000,
			want:      0,
		},
		{
			name:      "overflow is clamped",
			delta:     math.MaxInt64,
			elapsedMs: 1,
			want:      math.MaxInt64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeClientTrafficDelta(tt.delta, tt.elapsedMs); got != tt.want {
				t.Fatalf("normalizeClientTrafficDelta(%d, %d) = %d, want %d", tt.delta, tt.elapsedMs, got, tt.want)
			}
		})
	}
}
