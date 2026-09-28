package model

import "testing"

func TestNormalizeHiddifySubURIUsesStandardHTTPSPort(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "regular subscription port is removed",
			input: "https://cdn.example.com:2096/BackupPath123/",
			want:  "https://cdn.example.com/BackupPath123/",
		},
		{
			name:  "http listener is upgraded to https 443",
			input: "http://cdn.example.com:2053/BackupPath123",
			want:  "https://cdn.example.com/BackupPath123/",
		},
		{
			name:  "explicit 443 is canonicalized",
			input: "https://cdn.example.com:443/BackupPath123/",
			want:  "https://cdn.example.com/BackupPath123/",
		},
		{
			name:  "ipv6 host keeps brackets and drops listener port",
			input: "https://[2001:db8::1]:2096/BackupPath123/",
			want:  "https://[2001:db8::1]/BackupPath123/",
		},
		{
			name:  "invalid non-http value is left untouched",
			input: "legacy-value",
			want:  "legacy-value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeHiddifySubURI(tt.input); got != tt.want {
				t.Fatalf("normalizeHiddifySubURI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestClientRecordAfterFindNormalizesOnlyHiddifyURL(t *testing.T) {
	record := ClientRecord{
		Email:         "hiddify_Alice_768e8bdd",
		SubID:         "768e8bdd-bee3-4442-9006-b26464148aaa",
		HiddifySubURI: "https://cdn.example.com:2096/BackupPath123/",
	}
	if err := record.AfterFind(nil); err != nil {
		t.Fatal(err)
	}
	if got, want := record.HiddifySubURI, "https://cdn.example.com/BackupPath123/"; got != want {
		t.Fatalf("HiddifySubURI = %q, want %q", got, want)
	}

	regular := ClientRecord{Email: "ordinary@example.com", SubID: "ordinary"}
	if err := regular.AfterFind(nil); err != nil {
		t.Fatal(err)
	}
	if regular.HiddifySubURI != "" {
		t.Fatalf("regular client unexpectedly received Hiddify URL %q", regular.HiddifySubURI)
	}
}
