package service

import (
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/sudoku"
)

func setupSudokuCredentials(t *testing.T) (*model.Inbound, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	setupConflictDB(t)
	binDir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", binDir)
	script := `#!/bin/sh
counter="$(dirname "$0")/counter"
n=0
[ ! -f "$counter" ] || n=$(cat "$counter")
if [ "$2" = "-more" ]; then
 printf '12:00:00 info [CLI] Split Private Key: %0128d\n' "$n"
else
 n=$((n+1))
 echo "$n" > "$counter"
 printf '12:00:00 info [CLI] Master Public Key: %064d\n' "$n"
 printf '12:00:00 info [CLI] Master Private Key: %064d\n' "$n"
fi
`
	if err := os.WriteFile(sudoku.GetBinaryPath(binDir), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	inbound := &model.Inbound{
		UserId: 1, Tag: "sudoku-credentials", Protocol: model.Sudoku, Port: 2443, Enable: true,
		Settings: `{"futureOption":{"keep":true},"httpmask":{"mode":"poll","futureMask":7},"clients":[{"email":"alice@example.com","enable":true,"futureClient":"keep"},{"email":"bob@example.com","enable":true}]}`,
	}
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatal(err)
	}
	return inbound, binDir
}

func readSudokuSettings(t *testing.T, inbound *model.Inbound) sudokuStoredSettings {
	t.Helper()
	if err := database.GetDB().First(inbound, inbound.Id).Error; err != nil {
		t.Fatal(err)
	}
	settings, err := normalizeSudokuSettings(inbound.Settings)
	if err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestSudokuCredentialsPreserveSettingsAndRevokeDisabledClient(t *testing.T) {
	inbound, binDir := setupSudokuCredentials(t)
	if err := EnsureSudokuCredentials(inbound.Id); err != nil {
		t.Fatal(err)
	}
	first := readSudokuSettings(t, inbound)
	var fields map[string]any
	if err := json.Unmarshal([]byte(inbound.Settings), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["futureOption"] == nil || fields["httpmask"].(map[string]any)["futureMask"] != float64(7) || fields["clients"].([]any)[0].(map[string]any)["futureClient"] != "keep" {
		t.Fatal("credential generation discarded settings")
	}
	if err := EnsureSudokuCredentials(inbound.Id); err != nil {
		t.Fatal(err)
	}
	unchanged := readSudokuSettings(t, inbound)
	if unchanged.Key != first.Key || unchanged.Clients[0].SudokuPrivateKey != first.Clients[0].SudokuPrivateKey {
		t.Fatal("stable credentials were rotated")
	}
	clients := fields["clients"].([]any)
	clients[1].(map[string]any)["enable"] = false
	body, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(inbound).Update("settings", string(body)).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureSudokuCredentials(inbound.Id); err != nil {
		t.Fatal(err)
	}
	rotated := readSudokuSettings(t, inbound)
	if rotated.Key == first.Key || rotated.Clients[0].SudokuPrivateKey == first.Clients[0].SudokuPrivateKey || rotated.Clients[1].SudokuPrivateKey != "" {
		t.Fatal("disabling a client did not revoke its keys and rotate the survivor")
	}
	pending, err := sudoku.CredentialRotationPending(binDir, inbound.Id)
	if err != nil || pending {
		t.Fatalf("pending rotation = %v, %v", pending, err)
	}
	roster, err := sudoku.ReadClientRoster(binDir, inbound.Id)
	if err != nil || len(roster) != 1 || roster[0] != "alice@example.com" {
		t.Fatalf("roster = %v, %v", roster, err)
	}
}

func TestSudokuCredentialsConcurrentEditIsNotOverwritten(t *testing.T) {
	inbound, binDir := setupSudokuCredentials(t)
	db := database.GetDB()
	const edited = `{"clients":[],"concurrentEdit":true}`
	const callback = "test:sudoku-concurrent-edit"
	changed := false
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if changed {
			return
		}
		changed = true
		if err := tx.Exec("UPDATE inbounds SET settings = ? WHERE id = ?", edited, inbound.Id).Error; err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
	err := EnsureSudokuCredentials(inbound.Id)
	if err == nil || !strings.Contains(err.Error(), "changed during credential generation") {
		t.Fatalf("expected concurrent edit conflict, got %v", err)
	}
	readSudokuSettings(t, inbound)
	if inbound.Settings != edited {
		t.Fatalf("concurrent settings lost: %s", inbound.Settings)
	}
	pending, err := sudoku.CredentialRotationPending(binDir, inbound.Id)
	if err != nil || !pending {
		t.Fatalf("pending rotation = %v, %v", pending, err)
	}
	if err := EnsureSudokuCredentials(inbound.Id); err != nil {
		t.Fatal(err)
	}
	pending, err = sudoku.CredentialRotationPending(binDir, inbound.Id)
	if err != nil || pending {
		t.Fatalf("recovery failed: %v, %v", pending, err)
	}
}

func TestSudokuCredentialsFailedSaveRemainsRecoverable(t *testing.T) {
	inbound, binDir := setupSudokuCredentials(t)
	db := database.GetDB()
	const callback = "test:sudoku-save-failure"
	failure := errors.New("injected credential save failure")
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) { tx.AddError(failure) }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
	if err := EnsureSudokuCredentials(inbound.Id); !errors.Is(err, failure) {
		t.Fatalf("save error = %v", err)
	}
	pending, err := sudoku.CredentialRotationPending(binDir, inbound.Id)
	if err != nil || !pending {
		t.Fatalf("pending rotation = %v, %v", pending, err)
	}
	desired, err := DesiredSudokuInstances()
	if err != nil || len(desired) != 0 {
		t.Fatalf("failed credentials were activated: %v, %v", desired, err)
	}
	if err := db.Callback().Update().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSudokuCredentials(inbound.Id); err != nil {
		t.Fatal(err)
	}
	pending, err = sudoku.CredentialRotationPending(binDir, inbound.Id)
	if err != nil || pending {
		t.Fatalf("recovery failed: %v, %v", pending, err)
	}
}
