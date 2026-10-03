package mtproto

import (
	"strings"
	"testing"
)

func TestValidateNativeSettings(t *testing.T) {
	for _, settings := range []string{`{}`, `{"fakeTlsDomain":"www.cloudflare.com","maskHost":"127.0.0.1","maskPort":8443,"maskProxyProtocol":2,"proxyProtocolTrustedCidrs":["10.0.0.0/8","::1/128"],"allowLegacyModes":false}`} {
		if err := ValidateSettings(settings); err != nil {
			t.Errorf("%s: %v", settings, err)
		}
	}
	for _, settings := range []string{`{"fakeTlsDomain":"bad\\hostname"}`, `{"maskHost":"host:443"}`, `{"maskPort":65536}`, `{"maskProxyProtocol":3}`, `{"proxyProtocolTrustedCidrs":["0.0.0.0"]}`, `{"unknownSniAction":"foo"}`, `{"preferIp":"foo"}`, `{"publicIpv4":"::1"}`, `{"tlsMask":"true"}`, `{"throttleMaxConnections":-1}`} {
		if err := ValidateSettings(settings); err == nil {
			t.Errorf("accepted %s", settings)
		}
	}
}

func TestNativeUsernameRoundTrip(t *testing.T) {
	for _, email := range []string{"alice", "alice@example.com", "Пользователь", "xui_reserved", strings.Repeat("a", 65)} {
		name := nativeUsername(email)
		if nativeUsername(name) == name && strings.HasPrefix(email, "xui_") {
			t.Fatal("reserved prefix may collide with generated aliases")
		}
		if name != nativeUsername(email) {
			t.Fatal("alias is not stable")
		}
		if panelUsername(name, nativeUserEmails([]SecretEntry{{Name: email}})) != email {
			t.Fatal("lost panel identity")
		}
		for _, r := range name {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
				t.Fatalf("unsafe alias %q", name)
			}
		}
	}
	if nativeUsername("alice") != "alice" {
		t.Fatal("simple names must remain compatible")
	}
	if nativeUsername("alice@example.com") == nativeUsername("bob@example.com") {
		t.Fatal("alias collision")
	}
}
