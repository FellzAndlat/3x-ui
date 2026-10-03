package runtime

import (
	"context"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestLocalSudokuMutationsStayOutOfCoreRuntime(t *testing.T) {
	restarts := 0
	apiLookups := 0
	pendingRestarts := 0
	local := NewLocal(LocalDeps{
		APIPort: func() int {
			apiLookups++
			return -1
		},
		CoreType: func() string { return "sing-box" },
		RestartCore: func(context.Context) error {
			restarts++
			return nil
		},
		SetNeedRestart: func() {
			pendingRestarts++
		},
	})
	inbound := &model.Inbound{Id: 77, Tag: "sudoku-443", Protocol: model.Sudoku, Enable: true}
	client := model.Client{Email: "alice@example.com", Enable: true}
	ctx := context.Background()

	checks := []struct {
		name string
		run  func() error
	}{
		{"add inbound", func() error { return local.AddInbound(ctx, inbound) }},
		{"delete inbound", func() error { return local.DelInbound(ctx, inbound) }},
		{"update inbound", func() error { return local.UpdateInbound(ctx, inbound, inbound) }},
		{"add user", func() error { return local.AddUser(ctx, inbound, map[string]any{"email": client.Email}) }},
		{"remove user", func() error { return local.RemoveUser(ctx, inbound, client.Email) }},
		{"add client", func() error { return local.AddClient(ctx, inbound, client) }},
		{"update user", func() error { return local.UpdateUser(ctx, inbound, client.Email, client) }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); err != nil {
				t.Fatal(err)
			}
		})
	}

	if apiLookups != 0 {
		t.Fatalf("Sudoku touched Xray API port %d times", apiLookups)
	}
	if restarts != 0 {
		t.Fatalf("Sudoku restarted sing-box %d times", restarts)
	}
	if pendingRestarts != 0 {
		t.Fatalf("Sudoku armed Xray restart %d times", pendingRestarts)
	}
}

func TestLocalSudokuTransitionToCoreInboundAppliesCoreOnce(t *testing.T) {
	restarts := 0
	local := NewLocal(LocalDeps{
		CoreType: func() string { return "sing-box" },
		RestartCore: func(context.Context) error {
			restarts++
			return nil
		},
	})
	oldInbound := &model.Inbound{Id: 1, Tag: "sudoku", Protocol: model.Sudoku, Enable: true}
	newInbound := &model.Inbound{Id: 1, Tag: "vless", Protocol: model.VLESS, Enable: true}

	if err := local.UpdateInbound(context.Background(), oldInbound, newInbound); err != nil {
		t.Fatal(err)
	}
	if restarts != 1 {
		t.Fatalf("core restart count = %d, want 1", restarts)
	}
}

func TestLocalCoreTransitionToSudokuAppliesCoreOnce(t *testing.T) {
	restarts := 0
	local := NewLocal(LocalDeps{
		CoreType: func() string { return "sing-box" },
		RestartCore: func(context.Context) error {
			restarts++
			return nil
		},
	})
	oldInbound := &model.Inbound{Id: 1, Tag: "vless", Protocol: model.VLESS, Enable: true}
	newInbound := &model.Inbound{Id: 1, Tag: "sudoku", Protocol: model.Sudoku, Enable: true}

	if err := local.UpdateInbound(context.Background(), oldInbound, newInbound); err != nil {
		t.Fatal(err)
	}
	if restarts != 1 {
		t.Fatalf("core restart count = %d, want 1", restarts)
	}
}
