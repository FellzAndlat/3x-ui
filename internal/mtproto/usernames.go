package mtproto

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Telemt's users API only accepts ASCII letters, digits, '.', '_' and '-'.
// Panel email labels can contain '@', Unicode or spaces. Keep their public
// identity while using a stable, collision-resistant native username.
func nativeUsername(email string) string {
	valid := email != "" && len(email) <= 64 && !strings.HasPrefix(email, "xui_")
	for _, r := range email {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			valid = false
		}
	}
	if valid {
		return email
	}
	hash := sha256.Sum256([]byte(email))
	return fmt.Sprintf("xui_%x", hash[:16])
}

func nativeUserEmails(secrets []SecretEntry) map[string]string {
	out := make(map[string]string, len(secrets))
	for _, e := range secrets {
		out[nativeUsername(e.Name)] = e.Name
	}
	return out
}

func panelUsername(name string, emails map[string]string) string {
	if email, ok := emails[name]; ok {
		return email
	}
	return name
}
