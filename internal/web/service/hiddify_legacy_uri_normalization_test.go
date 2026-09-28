package service

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestGetHiddifySubscriptionURIsNormalizesStoredListenerPort(t *testing.T) {
	setupConflictDB(t)
	const id = "768e8bdd-bee3-4442-9006-b26464148aaa"
	if err := database.GetDB().Create(&model.ClientRecord{
		Email:         "hiddify_Alice_768e8bdd",
		UUID:          id,
		SubID:         id,
		HiddifySubURI: "https://cdn.example.com:2096/BackupPath123/",
	}).Error; err != nil {
		t.Fatal(err)
	}

	urls, err := (&SettingService{}).GetHiddifySubscriptionURIs()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := urls[id], "https://cdn.example.com/BackupPath123/"; got != want {
		t.Fatalf("Hiddify subscription URL = %q, want %q", got, want)
	}
}
