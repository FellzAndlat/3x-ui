package database

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestClampedAddExprSQLite(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	expr := ClampedAddExpr("value")
	if got := strings.Count(expr, "?"); got != 1 {
		t.Fatalf("ClampedAddExpr placeholders = %d, want 1: %s", got, expr)
	}
	if err := db.Exec(`CREATE TABLE traffic_clamp_test (value INTEGER NOT NULL)`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}

	tests := []struct {
		name   string
		stored int64
		delta  int64
		want   int64
	}{
		{name: "normal", stored: 100, delta: 25, want: 125},
		{name: "near max is clipped before addition", stored: TrafficMax - 3, delta: 10, want: TrafficMax},
		{name: "negative delta cannot decrement counter", stored: 100, delta: -25, want: 100},
		{name: "negative stored value is repaired", stored: -10, delta: 25, want: 25},
		{name: "stored value above max is repaired", stored: TrafficMax + 1000, delta: 1, want: TrafficMax},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := db.Exec(`DELETE FROM traffic_clamp_test`).Error; err != nil {
				t.Fatalf("clear table: %v", err)
			}
			if err := db.Exec(`INSERT INTO traffic_clamp_test(value) VALUES (?)`, tt.stored).Error; err != nil {
				t.Fatalf("insert: %v", err)
			}
			var got int64
			query := fmt.Sprintf("SELECT %s FROM traffic_clamp_test", expr)
			if err := db.Raw(query, tt.delta).Scan(&got).Error; err != nil {
				t.Fatalf("query: %v", err)
			}
			if got != tt.want {
				t.Fatalf("stored=%d delta=%d: got %d, want %d", tt.stored, tt.delta, got, tt.want)
			}
		})
	}
}

func TestClampedAddExprPostgresShape(t *testing.T) {
	const col = "traffic"
	expr := clampedAddExpr(col, true)
	if got := strings.Count(expr, "?"); got != 1 {
		t.Fatalf("postgres expression placeholders = %d, want 1: %s", got, expr)
	}
	if strings.Contains(expr, col+" + ?") {
		t.Fatalf("postgres expression performs unsafe addition before clamp: %s", expr)
	}
}

func TestClientTrafficEnableMergeExprPostgresUsesWideQuotaMath(t *testing.T) {
	expr := clientTrafficEnableMergeExpr(true)
	if got := strings.Count(expr, "?"); got != 6 {
		t.Fatalf("postgres quota expression placeholders = %d, want 6: %s", got, expr)
	}
	if !strings.Contains(expr, "CAST(up AS NUMERIC)") ||
		!strings.Contains(expr, "CAST(down AS NUMERIC)") ||
		strings.Contains(expr, "up + ? + down + ?") {
		t.Fatalf("postgres quota expression still uses overflow-prone BIGINT addition: %s", expr)
	}
}
