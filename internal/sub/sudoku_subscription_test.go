package sub

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/sudoku"
)

func TestSudokuSubscriptionRepairsMissingClientKey(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	binDir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", binDir)
	master := strings.Repeat("a", 64)
	public := strings.Repeat("b", 64)
	split := strings.Repeat("c", 128)
	program := "#!/bin/sh\nif [ \"$2\" = \"-more\" ]; then\n echo 'Split Private Key: " + split + "'\nelse\n echo 'Master Public Key: " + public + "'\n echo 'Master Private Key: " + master + "'\nfi\n"
	if err := os.WriteFile(sudoku.GetBinaryPath(binDir), []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	const email, subID = "sudoku@example.com", "sudoku-sub"
	inbound := &model.Inbound{
		UserId: 1, Tag: "sudoku-test", Enable: true, Port: 2443, Protocol: model.Sudoku,
		Settings:       `{"clients":[{"email":"sudoku@example.com","subId":"sudoku-sub","enable":true}]}`,
		StreamSettings: `{"network":"tcp","security":"none"}`,
	}
	db := database.GetDB()
	if err := db.Create(inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := &model.ClientRecord{Email: email, SubID: subID, Enable: true}
	if err := db.Create(client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inbound.Id}).Error; err != nil {
		t.Fatal(err)
	}
	links, _, _, _, err := NewSubService("").GetSubs(subID, "proxy.example.com")
	if err != nil || len(links) != 1 || !strings.HasPrefix(links[0], "sudoku://") {
		t.Fatalf("subscription links = %q, err = %v", links, err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(links[0], "sudoku://"))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["k"] != split || payload["h"] != "proxy.example.com" || payload["p"] != float64(2443) {
		t.Fatalf("unexpected Sudoku subscription payload: %#v", payload)
	}
	clientLinks := NewLinkProvider().LinksForClient("proxy.example.com", inbound, email)
	if len(clientLinks) != 1 || clientLinks[0] != links[0] {
		t.Fatalf("client links = %q, want %q", clientLinks, links)
	}
}
