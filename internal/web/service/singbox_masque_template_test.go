package service

import (
	"reflect"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestManagedMASQUEEndpointsOverrideTemplate(t *testing.T) {
	stale := map[string]any{"type": "masque-server", "tag": "active", "users": "old-password"}
	disabled := map[string]any{"type": "masque-server", "tag": "disabled"}
	noUsers := map[string]any{"type": "masque-server", "tag": "no-users"}
	manual := map[string]any{"type": "masque-server", "tag": "manual"}
	wireguard := map[string]any{"type": "wireguard", "tag": "wg"}
	generated := map[string]any{"type": "masque-server", "tag": "active", "users": "new-password"}
	tags := map[string]bool{"active": true, "disabled": true, "no-users": true}
	for _, tt := range []struct {
		name      string
		generated []map[string]any
		want      []map[string]any
	}{
		{"active credentials", []map[string]any{generated}, []map[string]any{manual, wireguard, generated}},
		{"all listeners disabled", nil, []map[string]any{manual, wireguard}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeManagedMASQUEEndpoints([]map[string]any{stale, disabled, noUsers, manual, wireguard}, tt.generated, tags)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("endpoints = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSingBoxConfigDoesNotRestoreDisabledMASQUE(t *testing.T) {
	setupBulkDB(t)
	inbound := &model.Inbound{Protocol: model.MASQUE, Tag: "disabled-masque", Enable: false, Port: 18443, Settings: `{}`}
	if err := database.GetDB().Create(inbound).Error; err != nil {
		t.Fatal(err)
	}
	if err := singBoxSettingService.SetSingBoxConfigTemplate(`{"endpoints":[{"type":"masque-server","tag":"disabled-masque","users":[{"username":"alice","password":"stale"}]},{"type":"wireguard","tag":"manual"}]}`); err != nil {
		t.Fatal(err)
	}
	cfg, err := (&SingBoxService{}).GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Endpoints) != 1 || cfg.Endpoints[0]["tag"] != "manual" {
		t.Fatalf("disabled listener restored from template: %#v", cfg.Endpoints)
	}
}
