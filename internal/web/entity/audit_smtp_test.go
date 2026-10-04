package entity

import "testing"

func TestAuditSMTPSettingsValidation(t *testing.T) {
	for _, recipients := range []string{"", `"Ops, team" <ops@example.org>, admin@example.org`} {
		s := &AllSetting{WebPort: 2053, SubPort: 2096, SmtpTo: recipients, SmtpEncryptionType: "starttls"}
		if err := s.CheckValid(); err != nil {
			t.Fatalf("valid recipients rejected: %v", err)
		}
	}
	for _, recipients := range []string{"bad-address", "ops@example.org\r\nBcc: extra@example.org"} {
		s := &AllSetting{WebPort: 2053, SubPort: 2096, SmtpTo: recipients}
		if err := s.CheckValid(); err == nil {
			t.Fatalf("invalid recipients accepted: %q", recipients)
		}
	}
	if err := (&AllSetting{WebPort: 2053, SubPort: 2096, SmtpEncryptionType: "unknown"}).CheckValid(); err == nil {
		t.Fatal("unknown SMTP mode accepted")
	}
}
