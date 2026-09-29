package service

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
)

func TestXrayTrafficReadRollbackRestoresBaselineAndUnlocks(t *testing.T) {
	s := &XrayService{}
	s.xrayAPI.StatsLastValues = map[string]int64{"user>>>alice>>>traffic>>>uplink": 200}
	s.xrayTrafficMu.Lock()

	read := &XrayTrafficRead{
		service: s,
		checkpoint: map[string]int64{
			"user>>>alice>>>traffic>>>uplink": 125,
		},
	}
	read.Rollback()

	if got := s.xrayAPI.StatsLastValues["user>>>alice>>>traffic>>>uplink"]; got != 125 {
		t.Fatalf("rollback baseline = %d, want 125", got)
	}
	if !s.xrayTrafficMu.TryLock() {
		t.Fatal("rollback did not release xrayTrafficMu")
	}
	s.xrayTrafficMu.Unlock()
}

func TestXrayTrafficReadCommitKeepsAdvancedBaselineAndUnlocks(t *testing.T) {
	s := &XrayService{}
	s.xrayAPI.StatsLastValues = map[string]int64{"inbound>>>in-443>>>traffic>>>downlink": 900}
	s.xrayTrafficMu.Lock()

	read := &XrayTrafficRead{
		service: s,
		checkpoint: map[string]int64{
			"inbound>>>in-443>>>traffic>>>downlink": 700,
		},
	}
	read.Commit()
	// A deferred rollback after Commit must be a no-op.
	read.Rollback()

	if got := s.xrayAPI.StatsLastValues["inbound>>>in-443>>>traffic>>>downlink"]; got != 900 {
		t.Fatalf("committed baseline = %d, want 900", got)
	}
	if !s.xrayTrafficMu.TryLock() {
		t.Fatal("commit did not release xrayTrafficMu")
	}
	s.xrayTrafficMu.Unlock()
}

func TestSaturatingTrafficDelta(t *testing.T) {
	tests := []struct {
		name string
		a    int64
		b    int64
		want int64
	}{
		{name: "normal", a: 10, b: 20, want: 30},
		{name: "negative delta ignored", a: 10, b: -5, want: 10},
		{name: "saturates", a: database.TrafficMax - 2, b: 10, want: database.TrafficMax},
		{name: "already saturated", a: database.TrafficMax, b: 1, want: database.TrafficMax},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := saturatingTrafficDelta(tt.a, tt.b); got != tt.want {
				t.Fatalf("saturatingTrafficDelta(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestClampTrafficDeltaToStored(t *testing.T) {
	tests := []struct {
		name   string
		stored int64
		delta  int64
		want   int64
	}{
		{name: "normal", stored: 100, delta: 25, want: 25},
		{name: "near max is clipped", stored: database.TrafficMax - 3, delta: 10, want: 3},
		{name: "already saturated", stored: database.TrafficMax, delta: 10, want: 0},
		{name: "negative delta ignored", stored: 100, delta: -1, want: 0},
		{name: "negative stored treated as zero", stored: -100, delta: 25, want: 25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampTrafficDeltaToStored(tt.stored, tt.delta); got != tt.want {
				t.Fatalf("clampTrafficDeltaToStored(%d, %d) = %d, want %d", tt.stored, tt.delta, got, tt.want)
			}
		})
	}
}
