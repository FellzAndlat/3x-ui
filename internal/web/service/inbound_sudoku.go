package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/sudoku"
)

var sudokuCredentialsMu sync.Mutex

func DisableUnavailableSudokuInbounds() error {
	binDir := config.GetBinFolderPath()
	if sudoku.IsInstalled(binDir) {
		return nil
	}
	return database.GetDB().
		Model(&model.Inbound{}).
		Where("protocol = ? AND node_id IS NULL AND enable = ?", model.Sudoku, true).
		Update("enable", false).Error
}

type sudokuHTTPMaskSettings struct {
	Disable   bool   `json:"disable"`
	Mode      string `json:"mode"`
	TLS       bool   `json:"tls"`
	Host      string `json:"host"`
	PathRoot  string `json:"pathRoot"`
	Multiplex string `json:"multiplex,omitempty"`
}

type sudokuStoredSettings struct {
	FallbackAddress    string                 `json:"fallbackAddress"`
	Key                string                 `json:"key"`
	AEAD               string                 `json:"aead"`
	SuspiciousAction   string                 `json:"suspiciousAction"`
	PaddingMin         int                    `json:"paddingMin"`
	PaddingMax         int                    `json:"paddingMax"`
	ASCII              string                 `json:"ascii"`
	CustomTable        string                 `json:"customTable"`
	CustomTables       []string               `json:"customTables"`
	EnablePureDownlink bool                   `json:"enablePureDownlink"`
	Multiplex          string                 `json:"multiplex"`
	HTTPMask           sudokuHTTPMaskSettings `json:"httpmask"`
	Clients            []model.Client         `json:"clients"`
}

func normalizeSudokuSettings(raw string) (sudokuStoredSettings, error) {
	settings := sudokuStoredSettings{
		AEAD:               "chacha20-poly1305",
		SuspiciousAction:   "silent",
		PaddingMin:         5,
		PaddingMax:         15,
		ASCII:              "prefer_entropy",
		EnablePureDownlink: true,
		Multiplex:          "off",
		HTTPMask:           sudokuHTTPMaskSettings{Mode: "auto"},
	}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &settings); err != nil {
			return settings, err
		}
	}
	if settings.AEAD == "" {
		settings.AEAD = "chacha20-poly1305"
	}
	if settings.SuspiciousAction == "" {
		settings.SuspiciousAction = "silent"
	}
	if settings.SuspiciousAction == "fallback" && strings.TrimSpace(settings.FallbackAddress) == "" {
		settings.SuspiciousAction = "silent"
	}
	if settings.PaddingMin < 0 || settings.PaddingMin > 100 {
		settings.PaddingMin = 5
	}
	if settings.PaddingMax < settings.PaddingMin || settings.PaddingMax > 100 {
		settings.PaddingMax = 15
	}
	if settings.PaddingMax < settings.PaddingMin {
		settings.PaddingMax = settings.PaddingMin
	}
	if settings.ASCII == "" {
		settings.ASCII = "prefer_entropy"
	}
	if settings.HTTPMask.Multiplex != "" {
		settings.Multiplex = settings.HTTPMask.Multiplex
	}
	if settings.Multiplex == "" {
		settings.Multiplex = "off"
	}
	settings.HTTPMask.Multiplex = settings.Multiplex
	if settings.HTTPMask.Mode == "" {
		settings.HTTPMask.Mode = "auto"
	}
	return settings, nil
}

func activeSudokuClientRoster(clients []model.Client) []string {
	seen := make(map[string]struct{}, len(clients))
	roster := make([]string, 0, len(clients))
	for _, client := range clients {
		if !client.Enable {
			continue
		}
		identity := strings.ToLower(strings.TrimSpace(client.Email))
		if identity == "" {
			continue
		}
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		roster = append(roster, identity)
	}
	sort.Strings(roster)
	return roster
}

func sudokuRosterSet(roster []string) map[string]struct{} {
	set := make(map[string]struct{}, len(roster))
	for _, item := range roster {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" {
			set[item] = struct{}{}
		}
	}
	return set
}

func EnsureSudokuCredentials(inboundID int) error {
	sudokuCredentialsMu.Lock()
	defer sudokuCredentialsMu.Unlock()

	db := database.GetDB()
	var inbound model.Inbound
	if err := db.First(&inbound, inboundID).Error; err != nil {
		return err
	}
	if inbound.Protocol != model.Sudoku || inbound.NodeID != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	binDir := config.GetBinFolderPath()
	if !sudoku.IsInstalled(binDir) {
		return os.ErrNotExist
	}
	binary := sudoku.GetBinaryPath(binDir)

	settings, err := normalizeSudokuSettings(inbound.Settings)
	if err != nil {
		return err
	}

	masterPrivate, readErr := sudoku.ReadMasterKey(binDir, inboundID)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	rotationPending, err := sudoku.CredentialRotationPending(binDir, inboundID)
	if err != nil {
		return err
	}

	currentRoster := activeSudokuClientRoster(settings.Clients)
	previousRoster, rosterErr := sudoku.ReadClientRoster(binDir, inboundID)
	rosterMissing := sudoku.ClientRosterMissing(rosterErr)
	if rosterErr != nil && !rosterMissing {
		return rosterErr
	}
	previousSet := sudokuRosterSet(previousRoster)
	currentSet := sudokuRosterSet(currentRoster)

	credentialsRevoked := false
	if rosterMissing {
		// Existing installations had no roster. Rotate once to establish a
		// trustworthy revocation baseline and invalidate keys belonging to users
		// that may have been removed before roster tracking existed.
		credentialsRevoked = sudoku.ValidPrivateKey(masterPrivate) && sudoku.ValidPublicKey(settings.Key)
	} else {
		for identity := range previousSet {
			if _, ok := currentSet[identity]; !ok {
				credentialsRevoked = true
				break
			}
		}
	}

	changed := false
	masterRotated := rotationPending || !sudoku.ValidPrivateKey(masterPrivate) || !sudoku.ValidPublicKey(settings.Key) || credentialsRevoked
	if masterRotated {
		// Persist a recovery marker before changing either side of the
		// public/private master-key pair. If any later step fails, the next
		// reconcile will rotate again instead of trusting mismatched state.
		if err := sudoku.BeginCredentialRotation(binDir, inboundID); err != nil {
			return err
		}
		publicKey, privateKey, keyErr := sudoku.GenerateMasterKey(ctx, binary)
		if keyErr != nil {
			return keyErr
		}
		settings.Key = publicKey
		masterPrivate = privateKey
		changed = true
	}

	for i := range settings.Clients {
		client := &settings.Clients[i]
		identity := strings.ToLower(strings.TrimSpace(client.Email))
		if !client.Enable || identity == "" {
			if client.SudokuPrivateKey != "" {
				client.SudokuPrivateKey = ""
				changed = true
			}
			continue
		}

		_, knownClient := previousSet[identity]
		needsKey := masterRotated || rosterMissing || !knownClient || !sudoku.ValidPrivateKey(client.SudokuPrivateKey)
		if !needsKey {
			continue
		}
		splitKey, keyErr := sudoku.GenerateSplitKey(ctx, binary, masterPrivate)
		if keyErr != nil {
			return keyErr
		}
		client.SudokuPrivateKey = splitKey
		changed = true
	}

	if changed {
		data, marshalErr := sudokuSettingsWithCredentials(inbound.Settings, settings)
		if marshalErr != nil {
			return marshalErr
		}
		result := db.Model(&model.Inbound{}).
			Where("id = ? AND settings = ? AND protocol = ? AND node_id IS NULL", inboundID, inbound.Settings, model.Sudoku).
			Update("settings", string(data))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("Sudoku inbound %d changed during credential generation; retry reconciliation", inboundID)
		}
	}

	// Commit the private half only after the database contains the matching
	// public key and client split keys. The pending marker keeps failures here
	// recoverable and causes DesiredSudokuInstances to fail closed meanwhile.
	if masterRotated {
		if err := sudoku.WriteMasterKey(binDir, inboundID, masterPrivate); err != nil {
			return err
		}
	}
	if err := sudoku.WriteClientRoster(binDir, inboundID, currentRoster); err != nil {
		return err
	}
	if masterRotated {
		if err := sudoku.CompleteCredentialRotation(binDir, inboundID); err != nil {
			return err
		}
	}
	return nil
}

func DesiredSudokuInstances() ([]sudoku.Instance, error) {
	if !sudoku.IsInstalled(config.GetBinFolderPath()) {
		return []sudoku.Instance{}, nil
	}

	var inbounds []*model.Inbound
	if err := database.GetDB().
		Where("protocol = ? AND node_id IS NULL AND enable = ?", model.Sudoku, true).
		Order("id ASC").
		Find(&inbounds).Error; err != nil {
		return nil, err
	}

	desired := make([]sudoku.Instance, 0, len(inbounds))
	for _, inbound := range inbounds {
		if err := EnsureSudokuCredentials(inbound.Id); err != nil {
			logger.Warningf("sudoku: credentials for inbound %d: %v", inbound.Id, err)
			continue
		}

		var current model.Inbound
		if err := database.GetDB().First(&current, inbound.Id).Error; err != nil {
			continue
		}
		if current.Protocol != model.Sudoku || !current.Enable || current.NodeID != nil {
			continue
		}
		settings, err := normalizeSudokuSettings(current.Settings)
		if err != nil {
			logger.Warningf("sudoku: invalid settings for inbound %d: %v", current.Id, err)
			continue
		}

		desired = append(desired, sudoku.Instance{
			ID:  current.Id,
			Tag: current.Tag,
			Config: sudoku.Config{
				Mode:               "server",
				Transport:          "tcp",
				LocalPort:          current.Port,
				FallbackAddr:       settings.FallbackAddress,
				Key:                settings.Key,
				AEAD:               settings.AEAD,
				SuspiciousAction:   settings.SuspiciousAction,
				PaddingMin:         settings.PaddingMin,
				PaddingMax:         settings.PaddingMax,
				ASCII:              settings.ASCII,
				CustomTable:        settings.CustomTable,
				CustomTables:       settings.CustomTables,
				EnablePureDownlink: settings.EnablePureDownlink,
				Multiplex:          settings.Multiplex,
				HTTPMask: sudoku.HTTPMaskConfig{
					Disable:   settings.HTTPMask.Disable,
					Mode:      settings.HTTPMask.Mode,
					TLS:       settings.HTTPMask.TLS,
					Host:      settings.HTTPMask.Host,
					PathRoot:  settings.HTTPMask.PathRoot,
					Multiplex: settings.HTTPMask.Multiplex,
				},
			},
		})
	}
	return desired, nil
}

// Credential maintenance owns only the master public key and client split
// keys. Preserve other settings, including fields added by newer clients.
func sudokuSettingsWithCredentials(raw string, settings sudokuStoredSettings) ([]byte, error) {
	fields := make(map[string]json.RawMessage)
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			return nil, err
		}
	}
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}
	var clients []map[string]json.RawMessage
	if value, ok := fields["clients"]; ok {
		if err := json.Unmarshal(value, &clients); err != nil {
			return nil, err
		}
	}
	if len(clients) != len(settings.Clients) {
		return nil, fmt.Errorf("Sudoku client list changed while rendering credentials")
	}
	for i, client := range settings.Clients {
		if clients[i] == nil {
			clients[i] = make(map[string]json.RawMessage)
		}
		if client.SudokuPrivateKey == "" {
			delete(clients[i], "sudokuPrivateKey")
		} else {
			value, err := json.Marshal(client.SudokuPrivateKey)
			if err != nil {
				return nil, err
			}
			clients[i]["sudokuPrivateKey"] = value
		}
	}
	key, err := json.Marshal(settings.Key)
	if err != nil {
		return nil, err
	}
	fields["key"] = key
	if _, ok := fields["clients"]; ok || len(clients) > 0 {
		value, err := json.Marshal(clients)
		if err != nil {
			return nil, err
		}
		fields["clients"] = value
	}
	return json.MarshalIndent(fields, "", "  ")
}

func RefreshSudokuCredentialsOnInbound(inbound *model.Inbound) error {
	if inbound == nil || inbound.Protocol != model.Sudoku {
		return nil
	}
	return EnsureSudokuCredentials(inbound.Id)
}
