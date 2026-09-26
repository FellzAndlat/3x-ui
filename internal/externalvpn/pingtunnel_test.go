package externalvpn

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestPingtunnelSettingsValidation(t *testing.T) {
	base := Settings{Key: 123456, Encrypt: "chacha20", EncryptKey: "secret", ConnectTimeout: 1000, Congestion: "bb"}
	for _, tc := range []struct {
		name string
		edit func(*Settings)
		valid bool
	}{
		{"encrypted", func(*Settings) {}, true},
		{"unencrypted", func(s *Settings) { s.Encrypt, s.EncryptKey = "", "" }, true},
		{"aes128", func(s *Settings) { s.Encrypt = "aes128" }, true},
		{"socks5", func(s *Settings) { s.Forward = "socks5://127.0.0.1:1080" }, true},
		{"http", func(s *Settings) { s.Forward = "http://proxy.example:8080" }, true},
		{"no congestion", func(s *Settings) { s.Congestion = "none" }, true},
		{"missing secret", func(s *Settings) { s.EncryptKey = "" }, false},
		{"invalid cipher", func(s *Settings) { s.Encrypt = "aes192" }, false},
		{"negative connections", func(s *Settings) { s.MaxConn = -1 }, false},
		{"negative timeout", func(s *Settings) { s.ConnectTimeout = -1 }, false},
		{"invalid proxy", func(s *Settings) { s.Forward = "socks5://proxy.example" }, false},
		{"invalid proxy port", func(s *Settings) { s.Forward = "http://proxy.example:70000" }, false},
		{"proxy credentials", func(s *Settings) { s.Forward = "http://user:pass@proxy.example:8080" }, false},
		{"invalid congestion", func(s *Settings) { s.Congestion = "cubic" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.edit(&s)
			err := (Instance{Protocol: model.Pingtunnel, Settings: s}).Validate()
			if (err == nil) != tc.valid {
				t.Fatalf("Validate() error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}
