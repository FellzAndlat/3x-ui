package runtime

import (
	"context"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestMieruUserChangesDoNotCallCore(t *testing.T) {
	l := NewLocal(LocalDeps{CoreType: func() string { return "sing-box" }, RestartCore: func(context.Context) error {
		t.Fatal("Mieru user changes must not restart sing-box")
		return nil
	}})
	ib := &model.Inbound{Protocol: model.Mieru}
	if err := l.AddUser(context.Background(), ib, map[string]any{"email": "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := l.RemoveUser(context.Background(), ib, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := l.UpdateUser(context.Background(), ib, "alice", model.Client{}); err != nil {
		t.Fatal(err)
	}
}
