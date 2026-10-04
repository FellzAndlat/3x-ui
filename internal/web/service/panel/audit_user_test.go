package panel

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/util/crypto"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
	"gorm.io/gorm"
)

func TestAuditUserUpdateAnd2FAAreAtomic(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "panel.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	db := database.GetDB()
	hash, err := crypto.HashPasswordAsBcrypt("original-password")
	if err != nil {
		t.Fatal(err)
	}
	user := &model.User{Username: "original", Password: hash, LoginEpoch: 3}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	settings := &service.SettingService{}
	if err := settings.SetTwoFactorEnable(true); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetTwoFactorToken("SECRET"); err != nil {
		t.Fatal(err)
	}
	svc := &UserService{}
	if err := svc.UpdateUser(user.Id+1000, "changed", "new-password"); err == nil {
		t.Fatal("missing user returned success")
	}
	const callback = "audit:fail-two-factor-save"
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "settings" {
			tx.AddError(errors.New("settings write failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
	if err := svc.UpdateUser(user.Id, "changed", "new-password"); err == nil {
		t.Fatal("2FA write failure was ignored")
	}
	var saved model.User
	if err := db.First(&saved, user.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Username != "original" || saved.LoginEpoch != 3 || !crypto.CheckPasswordHash(saved.Password, "original-password") {
		t.Fatal("failed 2FA update committed user changes")
	}
	if enabled, err := settings.GetTwoFactorEnable(); err != nil || !enabled {
		t.Fatal("failed update disabled 2FA")
	}
	if token, err := settings.GetTwoFactorToken(); err != nil || token != "SECRET" {
		t.Fatal("failed update cleared 2FA token")
	}
	if err := db.Callback().Update().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateUser(user.Id, "changed", "new-password"); err != nil {
		t.Fatal(err)
	}
	if enabled, err := settings.GetTwoFactorEnable(); err != nil || enabled {
		t.Fatal("successful update did not reset 2FA")
	}
	if err := db.First(&saved, user.Id).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Username != "changed" || saved.LoginEpoch != 4 || !crypto.CheckPasswordHash(saved.Password, "new-password") {
		t.Fatal("successful credentials update not persisted")
	}
}
