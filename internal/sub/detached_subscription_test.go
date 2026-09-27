package sub

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestGetSubsKeepsDetachedClientAddressable(t *testing.T) {
	initSubDB(t)
	db := database.GetDB()

	const subID = "b1337b29-8d60-4491-a468-c2bf120cb878"
	rec := &model.ClientRecord{
		Email:  "restored-hiddify@example.com",
		SubID:  subID,
		UUID:   subID,
		Enable: true,
	}
	if err := db.Create(rec).Error; err != nil {
		t.Fatalf("seed detached client: %v", err)
	}

	svc := NewSubService("")
	subs, emails, lastOnline, traffic, err := svc.getSubs(subID)
	if err != nil {
		t.Fatalf("getSubs(%q): %v", subID, err)
	}
	if subs == nil {
		t.Fatal("detached existing client returned nil subscription; controller would respond 404")
	}
	if len(subs) != 0 {
		t.Fatalf("detached client subscription = %v, want empty subscription", subs)
	}
	if len(emails) != 0 {
		t.Fatalf("detached client link emails = %v, want none until a connection is attached", emails)
	}
	if lastOnline != 0 {
		t.Fatalf("detached client lastOnline = %d, want 0", lastOnline)
	}
	if !traffic.Enable {
		t.Fatal("enabled detached client should keep subscription userinfo enabled")
	}

	missing, _, _, _, err := svc.getSubs("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("getSubs(missing): %v", err)
	}
	if missing != nil {
		t.Fatalf("unknown SubID returned %#v, want nil so controller keeps 404", missing)
	}
}
