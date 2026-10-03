package gateway

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

const singBoxBackupPath = "/etc/x-ui/gateway-singbox-backup.json"

func decodeSingBoxTemplate(raw string) (map[string]any, error) {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse sing-box template: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("parse sing-box template: top-level JSON value must be an object")
	}
	return cfg, nil
}

func loadSingBoxTemplateRaw() (string, error) {
	if err := database.InitDB(config.GetDBPath()); err != nil {
		return "", fmt.Errorf("initialize database: %w", err)
	}

	settings := &service.SettingService{}
	raw, err := settings.GetSingBoxConfigTemplate()
	if err != nil {
		return "", fmt.Errorf("get sing-box template: %w", err)
	}
	return raw, nil
}

func loadSingBoxTemplate() (map[string]any, string, error) {
	raw, err := loadSingBoxTemplateRaw()
	if err != nil {
		return nil, "", err
	}
	if raw == "" {
		return map[string]any{}, raw, nil
	}

	cfg, err := decodeSingBoxTemplate(raw)
	if err != nil {
		return nil, "", err
	}
	return cfg, raw, nil
}

func saveSingBoxTemplateRaw(raw string) error {
	settings := &service.SettingService{}
	if err := settings.SetSingBoxConfigTemplate(raw); err != nil {
		return fmt.Errorf("save sing-box template: %w", err)
	}
	return nil
}

func saveSingBoxTemplate(cfg map[string]any) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sing-box template: %w", err)
	}
	return saveSingBoxTemplateRaw(string(data))
}

func singBoxBackupExists() (bool, error) {
	_, err := os.Stat(singBoxBackupPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("check sing-box gateway backup: %w", err)
}

func readSingBoxBackup() (string, error) {
	data, err := os.ReadFile(singBoxBackupPath)
	if err != nil {
		return "", fmt.Errorf("read sing-box gateway backup: %w", err)
	}
	return string(data), nil
}

func createSingBoxBackup(raw string) error {
	file, err := os.OpenFile(singBoxBackupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("sing-box gateway backup already exists at %s; disable Gateway Mode first", singBoxBackupPath)
		}
		return fmt.Errorf("create sing-box gateway backup: %w", err)
	}

	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(singBoxBackupPath)
	}
	if _, err := file.WriteString(raw); err != nil {
		cleanup()
		return fmt.Errorf("write sing-box gateway backup: %w", err)
	}
	if err := file.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync sing-box gateway backup: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(singBoxBackupPath)
		return fmt.Errorf("close sing-box gateway backup: %w", err)
	}
	return nil
}

func removeSingBoxBackup() error {
	if err := os.Remove(singBoxBackupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove sing-box gateway backup: %w", err)
	}
	return nil
}

func getSingBoxStateUnlocked() (State, error) {
	raw, err := loadSingBoxTemplateRaw()
	if err != nil {
		return State{}, err
	}
	backup, err := singBoxBackupExists()
	if err != nil {
		return State{}, err
	}

	// An empty template is a valid unconfigured state. Gateway operations can
	// create their own objects from an empty config and restore the original
	// empty value from backup when Gateway Mode is disabled.
	if raw == "" {
		return State{
			Enabled:      backup,
			Configured:   false,
			BackupExists: backup,
		}, nil
	}

	cfg, err := decodeSingBoxTemplate(raw)
	if err != nil {
		return State{}, err
	}
	inbound := hasSingBoxGatewayInbound(cfg)
	sniff := hasSingBoxGatewaySniffRule(cfg)
	return State{
		// Any Gateway-owned object is considered enabled. This keeps partial or
		// interrupted state recoverable through Disable instead of trapping the
		// UI between a rejected Enable and a rejected Disable.
		Enabled:      hasSingBoxGatewayArtifacts(cfg) || backup,
		Configured:   inbound && sniff,
		BackupExists: backup,
	}, nil
}

func GetSingBoxState() (State, error) {
	operationMu.Lock()
	defer operationMu.Unlock()
	return getSingBoxStateUnlocked()
}

func EnableSingBox() error {
	operationMu.Lock()
	defer operationMu.Unlock()

	cfg, raw, err := loadSingBoxTemplate()
	if err != nil {
		return err
	}
	backup, err := singBoxBackupExists()
	if err != nil {
		return err
	}
	if hasSingBoxGatewayArtifacts(cfg) || backup {
		return fmt.Errorf("Gateway Mode is already enabled or requires cleanup")
	}

	if err := applySingBoxGatewayConfig(cfg); err != nil {
		return err
	}
	if err := createSingBoxBackup(raw); err != nil {
		return err
	}
	if err := saveSingBoxTemplate(cfg); err != nil {
		if cleanupErr := removeSingBoxBackup(); cleanupErr != nil {
			return fmt.Errorf("%w; cleanup failed: %w", err, cleanupErr)
		}
		return err
	}

	fmt.Println("sing-box Gateway configuration enabled.")
	return nil
}

func DisableSingBox() error {
	operationMu.Lock()
	defer operationMu.Unlock()

	cfg, _, err := loadSingBoxTemplate()
	if err != nil {
		return err
	}
	backup, err := singBoxBackupExists()
	if err != nil {
		return err
	}
	artifacts := hasSingBoxGatewayArtifacts(cfg)
	if !artifacts && !backup {
		return fmt.Errorf("Gateway Mode is not enabled")
	}

	if backup {
		backupRaw, err := readSingBoxBackup()
		if err != nil {
			return err
		}
		// Restore the exact original value when Gateway started from an empty
		// template, or when the current config no longer contains Gateway-owned
		// objects and the backup is the only recoverable state left.
		if backupRaw == "" || !artifacts {
			if err := saveSingBoxTemplateRaw(backupRaw); err != nil {
				return err
			}
			if err := removeSingBoxBackup(); err != nil {
				return err
			}
			fmt.Println("sing-box Gateway configuration disabled.")
			return nil
		}
	}

	if artifacts {
		changed, err := removeSingBoxGatewayConfig(cfg)
		if err != nil {
			return err
		}
		if changed {
			if err := saveSingBoxTemplate(cfg); err != nil {
				return err
			}
		}
	}

	if backup {
		if err := removeSingBoxBackup(); err != nil {
			return err
		}
	}

	fmt.Println("sing-box Gateway configuration disabled.")
	return nil
}
