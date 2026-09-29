package service

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const telemtSubscriptionUserPrefix = "sub_"

var (
	telemtSubscriptionProfileMu         sync.Mutex
	errTelemtSubscriptionAPIUnsupported = errors.New("telemt: users API is unavailable")
)

// telemtSubscriptionUsername maps a subscription identifier to a stable Telemt
// username without exposing the subscription token itself in telemt.toml.
func telemtSubscriptionUsername(subID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(subID)))
	return telemtSubscriptionUserPrefix + hex.EncodeToString(sum[:12])
}

func isTelemtSubscriptionUsername(username string) bool {
	username = strings.TrimSpace(username)
	if !strings.HasPrefix(username, telemtSubscriptionUserPrefix) || len(username) != len(telemtSubscriptionUserPrefix)+24 {
		return false
	}
	_, err := hex.DecodeString(username[len(telemtSubscriptionUserPrefix):])
	return err == nil
}

// EnsureSubscriptionProxy returns the stable Telemt proxy assigned to subID,
// creating it on first use. Repeated calls keep the same username and secret.
// It never starts a stopped Telemt service: opening a public subscription page
// must not override the administrator's explicit service state.
func (TelemtService) EnsureSubscriptionProxy(subID, host string) (TelemtProxy, error) {
	subID = strings.TrimSpace(subID)
	host = strings.TrimSpace(host)
	if subID == "" {
		return TelemtProxy{}, errors.New("telemt: subscription id is required")
	}
	if host == "" || strings.ContainsAny(host, " \t\n\r\"") {
		return TelemtProxy{}, errors.New("telemt: public host is required")
	}
	if _, err := os.Stat(telemtBinaryPath); err != nil {
		return TelemtProxy{}, fmt.Errorf("telemt: proxy engine is not installed: %w", err)
	}

	telemtSubscriptionProfileMu.Lock()
	defer telemtSubscriptionProfileMu.Unlock()

	if systemctl("is-active", "--quiet", telemtServiceName) != nil {
		return TelemtProxy{}, errors.New("telemt: service is not active")
	}
	if err := ensureTelemtConfig(); err != nil {
		return TelemtProxy{}, err
	}
	original, err := os.ReadFile(telemtConfigPath)
	if err != nil {
		return TelemtProxy{}, err
	}

	var raw struct {
		Server struct {
			Port int `toml:"port"`
		} `toml:"server"`
		General struct {
			Modes struct {
				TLS bool `toml:"tls"`
			} `toml:"modes"`
		} `toml:"general"`
		Access struct {
			Users map[string]string `toml:"users"`
		} `toml:"access"`
	}
	if err := toml.Unmarshal(original, &raw); err != nil {
		return TelemtProxy{}, fmt.Errorf("telemt: parse config: %w", err)
	}

	username := telemtSubscriptionUsername(subID)
	secret := strings.TrimSpace(raw.Access.Users[username])
	if secret == "" {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return TelemtProxy{}, fmt.Errorf("telemt: generate subscription secret: %w", err)
		}
		secret = hex.EncodeToString(buf)

		if err := createTelemtSubscriptionUser(username, secret); err != nil {
			if !errors.Is(err, errTelemtSubscriptionAPIUnsupported) {
				return TelemtProxy{}, err
			}
			if err := createTelemtSubscriptionUserLegacy(original, username, secret); err != nil {
				return TelemtProxy{}, err
			}
		}
	}

	link, err := telemtGeneratedLink(username, raw.General.Modes.TLS)
	if err != nil {
		return TelemtProxy{}, fmt.Errorf("telemt: failed to obtain subscription link: %w", err)
	}
	if parsed, err := url.Parse(link); err == nil {
		query := parsed.Query()
		if effectiveSecret := strings.TrimSpace(query.Get("secret")); effectiveSecret != "" {
			secret = effectiveSecret
		}
		query.Set("server", host)
		parsed.RawQuery = query.Encode()
		link = parsed.String()
	}

	return TelemtProxy{
		Name:   username,
		Secret: secret,
		Host:   host,
		Port:   raw.Server.Port,
		TLS:    raw.General.Modes.TLS,
		Link:   link,
	}, nil
}

// DeleteSubscriptionProxy revokes the deterministic Telemt user assigned to a
// subscription. It never starts a stopped Telemt service. When the users API is
// unavailable, it removes the account from telemt.toml directly and only
// restarts Telemt when the service was already active.
func (TelemtService) DeleteSubscriptionProxy(subID string) error {
	subID = strings.TrimSpace(subID)
	if subID == "" {
		return nil
	}

	telemtSubscriptionProfileMu.Lock()
	defer telemtSubscriptionProfileMu.Unlock()

	username := telemtSubscriptionUsername(subID)
	active := systemctl("is-active", "--quiet", telemtServiceName) == nil
	var apiErr error
	if active {
		apiErr = deleteTelemtSubscriptionUser(username)
		if apiErr == nil {
			return nil
		}
	}

	if err := deleteTelemtSubscriptionUserLegacy(username, active); err != nil {
		if apiErr != nil {
			return fmt.Errorf("telemt: revoke subscription user via API (%v) and config fallback: %w", apiErr, err)
		}
		return err
	}
	return nil
}

// createTelemtSubscriptionUser uses Telemt's users API when available. Modern
// Telemt versions update access.users atomically and apply runtime admission
// without restarting the whole proxy process.
func createTelemtSubscriptionUser(username, secret string) error {
	body, err := json.Marshal(map[string]string{
		"username": username,
		"secret":   secret,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:9091/v1/users", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telemt: create subscription user request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("telemt: create subscription user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusConflict {
		// A concurrent request may have created the same deterministic user.
		// The following GET resolves the effective secret/link from Telemt.
		return nil
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return errTelemtSubscriptionAPIUnsupported
	}
	message, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if detail := strings.TrimSpace(string(message)); detail != "" {
		return fmt.Errorf("telemt: create subscription user: HTTP %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("telemt: create subscription user: HTTP %d", resp.StatusCode)
}

func deleteTelemtSubscriptionUser(username string) error {
	req, err := http.NewRequest(http.MethodDelete, "http://127.0.0.1:9091/v1/users/"+url.PathEscape(username), nil)
	if err != nil {
		return fmt.Errorf("telemt: delete subscription user request: %w", err)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("telemt: delete subscription user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	// A 404 can mean either that the user is already gone or that an older
	// Telemt build does not expose the users endpoint. The config fallback is
	// idempotent, so let the caller use it in both cases.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return errTelemtSubscriptionAPIUnsupported
	}
	message, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if detail := strings.TrimSpace(string(message)); detail != "" {
		return fmt.Errorf("telemt: delete subscription user: HTTP %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("telemt: delete subscription user: HTTP %d", resp.StatusCode)
}

// createTelemtSubscriptionUserLegacy keeps compatibility with Telemt builds
// predating POST /v1/users. The service is already known to be active; this
// fallback rewrites only access.users and restarts with rollback on failure.
func createTelemtSubscriptionUserLegacy(original []byte, username, secret string) error {
	text := string(original)
	section := "[access.users]"
	idx := strings.Index(text, section)
	if idx < 0 {
		return errors.New("telemt: access.users section is missing")
	}
	insertAt := len(text)
	if next := strings.Index(text[idx+len(section):], "\n["); next >= 0 {
		insertAt = idx + len(section) + next + 1
	}
	entry := fmt.Sprintf("%s = \"%s\"\n", username, secret)
	text = text[:insertAt] + entry + text[insertAt:]
	if err := os.WriteFile(telemtConfigPath, []byte(text), 0o600); err != nil {
		return err
	}

	rollback := func() {
		_ = os.WriteFile(telemtConfigPath, original, 0o600)
		_ = systemctl("daemon-reload")
	}
	if err := systemctl("daemon-reload"); err != nil {
		rollback()
		return err
	}
	if err := systemctl("restart", telemtServiceName); err != nil {
		rollback()
		_ = systemctl("restart", telemtServiceName)
		return fmt.Errorf("telemt: subscription profile was rejected: %w", err)
	}
	return nil
}

func deleteTelemtSubscriptionUserLegacy(username string, restart bool) error {
	original, err := os.ReadFile(telemtConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("telemt: read config while revoking subscription user: %w", err)
	}

	var raw struct {
		Access struct {
			Users map[string]string `toml:"users"`
		} `toml:"access"`
	}
	if err := toml.Unmarshal(original, &raw); err != nil {
		return fmt.Errorf("telemt: parse config while revoking subscription user: %w", err)
	}
	if _, exists := raw.Access.Users[username]; !exists {
		return nil
	}

	lines := strings.SplitAfter(string(original), "\n")
	inUsers := false
	removed := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inUsers = trimmed == "[access.users]"
		}
		if inUsers {
			if eq := strings.Index(trimmed, "="); eq >= 0 && strings.TrimSpace(trimmed[:eq]) == username {
				removed = true
				continue
			}
		}
		out = append(out, line)
	}
	if !removed {
		return fmt.Errorf("telemt: subscription user %q exists in parsed config but its TOML entry was not found", username)
	}

	if err := os.WriteFile(telemtConfigPath, []byte(strings.Join(out, "")), 0o600); err != nil {
		return fmt.Errorf("telemt: persist revoked subscription user: %w", err)
	}
	if !restart {
		return nil
	}
	if err := systemctl("restart", telemtServiceName); err != nil {
		return fmt.Errorf("telemt: restart after revoking subscription user: %w", err)
	}
	return nil
}
