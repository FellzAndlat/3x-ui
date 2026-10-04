package masque

import (
	"encoding/json"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"testing"
)

func TestPrepareDefaultsAndPreservesCredentials(t *testing.T) {
	ib := &model.Inbound{Protocol: model.MASQUE, Settings: `{"clients":[{"email":"alice","enable":true}]}`}
	if err := Prepare(ib, ""); err != nil {
		t.Fatal(err)
	}
	s, err := Parse(ib.Settings)
	if err != nil {
		t.Fatal(err)
	}
	if s.MTU != 1280 || s.Path != DefaultPath || len(s.Address) != 2 || !s.TLS.Enabled || s.Clients[0].Password == "" {
		t.Fatalf("incomplete defaults: %+v", s)
	}
	previous := ib.Settings
	password := s.Clients[0].Password
	s.Clients[0].Password = ""
	data, _ := json.Marshal(s)
	ib.Settings = string(data)
	if err := Prepare(ib, previous); err != nil {
		t.Fatal(err)
	}
	s, _ = Parse(ib.Settings)
	if s.Clients[0].Password != password {
		t.Fatal("update changed credentials")
	}
}

func TestInvalidSettings(t *testing.T) {
	for _, raw := range []string{`{"version":[4]}`, `{"version":[3,3]}`, `{"address":["bad"]}`, `{"address":["10.0.0.1/24","10.1.0.1/24"]}`, `{"address":["10.0.0.1/32"]}`, `{"mtu":1279}`, `{"advertiseRoutes":["bad"]}`, `{"path":"https://server/"}`, `{"tls":{"certificatePath":"cert"}}`} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
