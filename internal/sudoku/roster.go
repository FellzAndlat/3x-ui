package sudoku

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func clientRosterPath(binDir string, id int) string {
	return filepath.Join(configDir(binDir), fmt.Sprintf("sudoku-%d.clients", id))
}

func ReadClientRoster(binDir string, id int) ([]string, error) {
	data, err := os.ReadFile(clientRosterPath(binDir, id))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	seen := make(map[string]struct{}, len(lines))
	roster := make([]string, 0, len(lines))
	for _, line := range lines {
		value := strings.ToLower(strings.TrimSpace(line))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		roster = append(roster, value)
	}
	sort.Strings(roster)
	return roster, nil
}

func WriteClientRoster(binDir string, id int, roster []string) error {
	seen := make(map[string]struct{}, len(roster))
	values := make([]string, 0, len(roster))
	for _, item := range roster {
		value := strings.ToLower(strings.TrimSpace(item))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	sort.Strings(values)
	body := strings.Join(values, "\n")
	if body != "" {
		body += "\n"
	}
	return writeAtomicFile(clientRosterPath(binDir, id), []byte(body), 0o600)
}

func ClientRosterMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
