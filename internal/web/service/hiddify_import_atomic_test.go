package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestHiddifyTwentyUsersImportWithoutInbounds(t *testing.T) {
	setupBulkDB(t)

	var users strings.Builder
	users.WriteString(`{"users":[{"uuid":"00000000-0000-4000-8000-000000000000","name":"default","enable":true,"is_active":true}`)
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&users, `,{"uuid":"00000000-0000-4000-8000-%012d","name":"User%d","enable":true,"is_active":true}`, i, i)
	}
	users.WriteString(`]}`)

	backup, preview, err := ParseHiddifyBackup(strings.NewReader(users.String()))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Users != 20 || len(preview.UserList) != 20 {
		t.Fatalf("preview = %d users, %d choices; want 20", preview.Users, len(preview.UserList))
	}

	selected := make([]string, 0, len(preview.UserList))
	for _, user := range preview.UserList {
		selected = append(selected, user.UUID)
	}
	items, err := backup.HiddifyClientsSelected(selected)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := (&ClientService{}).ImportClients(nil, items)
	if err != nil || result.Created != 20 || len(result.Skipped) != 0 {
		t.Fatalf("import = %+v, %v; want 20 created", result, err)
	}

	var count int64
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("group_name = ?", "Hiddify").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 20 {
		t.Fatalf("stored Hiddify clients = %d, want 20", count)
	}
}

func TestImportClientsOrphanBatchRollsBackOnWriteError(t *testing.T) {
	setupBulkDB(t)
	db := database.GetDB()
	if err := db.Exec(`
		CREATE TRIGGER fail_atomic_client_import
		BEFORE INSERT ON clients
		WHEN NEW.email = 'fail@atomic'
		BEGIN
			SELECT RAISE(ABORT, 'forced import failure');
		END;
	`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	items := []ClientCreatePayload{
		{Client: model.Client{Email: "first@atomic", SubID: "atomic-first", Enable: true}},
		{Client: model.Client{Email: "fail@atomic", SubID: "atomic-fail", Enable: true}},
		{Client: model.Client{Email: "last@atomic", SubID: "atomic-last", Enable: true}},
	}
	result, _, err := (&ClientService{}).ImportClients(nil, items)
	if err == nil || !strings.Contains(err.Error(), "fail@atomic") {
		t.Fatalf("ImportClients error = %v, want failing client context", err)
	}
	if result.Created != 0 {
		t.Fatalf("reported created = %d after rollback, want 0", result.Created)
	}

	var count int64
	if err := db.Model(&model.ClientRecord{}).
		Where("email IN ?", []string{"first@atomic", "fail@atomic", "last@atomic"}).
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("partial orphan import committed %d clients; want rollback", count)
	}
}
