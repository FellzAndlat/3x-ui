package service

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

// ReconcileSubscriptionProxies makes the deterministic Telemt accounts match
// every subscription already stored by the panel. Enabling provisions missing
// accounts for all saved subscription IDs; disabling revokes every generated
// subscription account, including stale accounts left by subscriptions that no
// longer exist in the database.
//
// The operation is intentionally idempotent so it can also be used to repair a
// partially-applied previous attempt without creating duplicate Telemt users.
func (TelemtService) ReconcileSubscriptionProxies(enabled bool, host string) error {
	if !enabled {
		return deleteAllTelemtSubscriptionUsers()
	}

	var subIDs []string
	if err := database.GetDB().
		Model(&model.ClientRecord{}).
		Where("TRIM(sub_id) <> ?", "").
		Distinct("sub_id").
		Order("sub_id ASC").
		Pluck("sub_id", &subIDs).Error; err != nil {
		return fmt.Errorf("telemt: list saved subscriptions: %w", err)
	}

	host = strings.TrimSpace(host)
	if host == "" && len(subIDs) > 0 {
		return fmt.Errorf("telemt: public host is required to enable subscription proxies")
	}

	failed := 0
	var firstErr error
	for _, subID := range subIDs {
		if _, err := (TelemtService{}).EnsureSubscriptionProxy(subID, host); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if failed == 0 {
		return nil
	}
	return fmt.Errorf("telemt: failed to create subscription proxies for %d of %d saved subscriptions: %w", failed, len(subIDs), firstErr)
}

// deleteAllTelemtSubscriptionUsers removes all panel-generated sub_* users, not
// only those that still have a ClientRecord. This guarantees that switching the
// feature off also cleans up orphaned users left by older versions.
func deleteAllTelemtSubscriptionUsers() error {
	telemtSubscriptionProfileMu.Lock()
	defer telemtSubscriptionProfileMu.Unlock()

	usernames, err := telemtSubscriptionUsernamesFromConfig()
	if err != nil {
		return err
	}
	if len(usernames) == 0 {
		return nil
	}

	active := systemctl("is-active", "--quiet", telemtServiceName) == nil
	if active {
		apiOK := true
		for _, username := range usernames {
			if err := deleteTelemtSubscriptionUser(username); err != nil {
				apiOK = false
				break
			}
		}
		if apiOK {
			return nil
		}
	}

	// Older Telemt versions do not expose DELETE /v1/users. Re-read the config
	// before the fallback so any successful API deletions or unrelated changes
	// made since the initial scan are not accidentally restored.
	return deleteAllTelemtSubscriptionUsersFromConfig(active)
}

func telemtSubscriptionUsernamesFromConfig() ([]string, error) {
	content, err := os.ReadFile(telemtConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("telemt: read config while listing subscription users: %w", err)
	}

	var raw struct {
		Access struct {
			Users map[string]string `toml:"users"`
		} `toml:"access"`
	}
	if err := toml.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("telemt: parse config while listing subscription users: %w", err)
	}
	return telemtSubscriptionUsernames(raw.Access.Users), nil
}

func telemtSubscriptionUsernames(users map[string]string) []string {
	usernames := make([]string, 0)
	for username := range users {
		if isTelemtSubscriptionUsername(username) {
			usernames = append(usernames, username)
		}
	}
	sort.Strings(usernames)
	return usernames
}

func removeTelemtSubscriptionUsersFromTOML(original []byte) ([]byte, int, error) {
	var raw struct {
		Access struct {
			Users map[string]string `toml:"users"`
		} `toml:"access"`
	}
	if err := toml.Unmarshal(original, &raw); err != nil {
		return nil, 0, fmt.Errorf("telemt: parse config while revoking subscription users: %w", err)
	}

	remaining := len(telemtSubscriptionUsernames(raw.Access.Users))
	if remaining == 0 {
		return original, 0, nil
	}

	lines := strings.SplitAfter(string(original), "\n")
	inUsers := false
	removed := 0
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inUsers = trimmed == "[access.users]"
		}
		if inUsers {
			if eq := strings.Index(trimmed, "="); eq >= 0 {
				username := strings.Trim(strings.TrimSpace(trimmed[:eq]), "\"'")
				if isTelemtSubscriptionUsername(username) {
					removed++
					continue
				}
			}
		}
		out = append(out, line)
	}
	if removed != remaining {
		return nil, removed, fmt.Errorf("telemt: found %d subscription users in parsed config but removed %d TOML entries", remaining, removed)
	}
	return []byte(strings.Join(out, "")), removed, nil
}

func deleteAllTelemtSubscriptionUsersFromConfig(restart bool) error {
	original, err := os.ReadFile(telemtConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("telemt: read config while revoking subscription users: %w", err)
	}

	updated, removed, err := removeTelemtSubscriptionUsersFromTOML(original)
	if err != nil {
		return err
	}
	if removed == 0 {
		return nil
	}
	if err := os.WriteFile(telemtConfigPath, updated, 0o600); err != nil {
		return fmt.Errorf("telemt: persist revoked subscription users: %w", err)
	}
	if !restart {
		return nil
	}
	if err := systemctl("restart", telemtServiceName); err != nil {
		_ = os.WriteFile(telemtConfigPath, original, 0o600)
		_ = systemctl("restart", telemtServiceName)
		return fmt.Errorf("telemt: restart after revoking subscription users: %w", err)
	}
	return nil
}
