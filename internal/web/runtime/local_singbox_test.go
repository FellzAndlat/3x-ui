package runtime

import (
	"context"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestLocalSingBoxMutationsDoNotUseXrayAPI(t *testing.T) {
	calls := 0
	apiLookups := 0
	l := NewLocal(LocalDeps{
		APIPort: func() int {
			apiLookups++
			return -1
		},
		CoreType: func() string { return "sing-box" },
		RestartCore: func(context.Context) error {
			calls++
			return nil
		},
	})
	ib := &model.Inbound{Id: 1, Tag: "vless-443", Protocol: model.VLESS, Enable: true}

	if err := l.AddInbound(context.Background(), ib); err != nil {
		t.Fatal(err)
	}
	if err := l.DelInbound(context.Background(), ib); err != nil {
		t.Fatal(err)
	}
	if err := l.AddUser(context.Background(), ib, map[string]any{"email": "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := l.RemoveUser(context.Background(), ib, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := l.UpdateInbound(context.Background(), ib, ib); err != nil {
		t.Fatal(err)
	}
	if err := l.UpdateUser(context.Background(), ib, "alice", model.Client{Email: "alice", Enable: true}); err != nil {
		t.Fatal(err)
	}
	if apiLookups != 0 {
		t.Fatalf("sing-box runtime touched Xray API port %d times", apiLookups)
	}
	if calls != 6 {
		t.Fatalf("restart calls = %d, want 6", calls)
	}
}

func TestLocalSingBoxTUICMutationsRestartCore(t *testing.T) {
	calls := 0
	l := NewLocal(LocalDeps{CoreType: func() string { return "sing-box" }, RestartCore: func(context.Context) error {
		calls++
		return nil
	}})
	ib := &model.Inbound{Id: 123, Tag: "tuic-443", Protocol: model.TUIC, Enable: true}
	if err := l.AddInbound(context.Background(), ib); err != nil {
		t.Fatal(err)
	}
	if err := l.UpdateInbound(context.Background(), ib, ib); err != nil {
		t.Fatal(err)
	}
	if err := l.DelInbound(context.Background(), ib); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("TUIC mutations restarted sing-box %d times, want 3", calls)
	}
}
