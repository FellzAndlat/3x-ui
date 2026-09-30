package gateway

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

const (
	backupPath = "/etc/x-ui/gateway-xray-backup.json"

	inboundTag  = "in-tproxy"
	outboundTag = "xui-gateway-direct"
	inboundPort = 52345
)

var tproxyInbound = map[string]any{
	"listen":   "127.0.0.1",
	"port":     inboundPort,
	"protocol": "dokodemo-door",
	"settings": map[string]any{
		"followRedirect": true,
		"network":        "tcp,udp",
	},
	"sniffing": map[string]any{
		"destOverride": []any{
			"http",
			"tls",
			"quic",
		},
		"enabled":   true,
		"routeOnly": true,
	},
	"streamSettings": map[string]any{
		"sockopt": map[string]any{
			"tproxy": "tproxy",
		},
	},
	"tag": inboundTag,
}

var gatewayDirectOutbound = map[string]any{
	"protocol": "freedom",
	"settings": map[string]any{
		"finalRules": []any{
			map[string]any{
				"action": "allow",
			},
		},
	},
	"tag": outboundTag,
}

func loadTemplate() (map[string]any, string, error) {
	if err := database.InitDB(config.GetDBPath()); err != nil {
		return nil, "", fmt.Errorf("initialize database: %w", err)
	}

	settings := &service.SettingService{}

	raw, err := settings.GetXrayConfigTemplate()
	if err != nil {
		return nil, "", fmt.Errorf("get Xray template: %w", err)
	}

	var cfg map[string]any

	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, "", fmt.Errorf("parse Xray template: %w", err)
	}

	return cfg, raw, nil
}

func saveTemplate(cfg map[string]any) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal Xray template: %w", err)
	}

	settings := &service.XraySettingService{}

	if err := settings.SaveXraySetting(string(data)); err != nil {
		return fmt.Errorf("save Xray template: %w", err)
	}

	return nil
}

func createBackup(raw string) error {
	if _, err := os.Stat(backupPath); err == nil {
		return fmt.Errorf(
			"gateway backup already exists at %s; disable Gateway Mode first",
			backupPath,
		)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check gateway backup: %w", err)
	}

	if err := os.WriteFile(backupPath, []byte(raw), 0600); err != nil {
		return fmt.Errorf("write gateway backup: %w", err)
	}

	return nil
}

func removeBackup() error {
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove gateway backup: %w", err)
	}
	return nil
}

func restoreBackup() error {
	raw, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read gateway backup: %w", err)
	}

	settings := &service.XraySettingService{}

	if err := settings.SaveXraySetting(string(raw)); err != nil {
		return fmt.Errorf("restore Xray template: %w", err)
	}

	return nil
}

func addTproxyInbound(cfg map[string]any) {
	inbounds, ok := cfg["inbounds"].([]any)

	if !ok {
		inbounds = []any{}
	}

	// Remove an old Gateway-owned inbound if present.
	filtered := make([]any, 0, len(inbounds)+1)

	for _, item := range inbounds {
		obj, ok := item.(map[string]any)
		if !ok {
			filtered = append(filtered, item)
			continue
		}

		tag, _ := obj["tag"].(string)

		if tag == inboundTag {
			continue
		}

		filtered = append(filtered, item)
	}

	filtered = append(filtered, tproxyInbound)

	cfg["inbounds"] = filtered
}

func addGatewayOutbound(cfg map[string]any) {
	outbounds, ok := cfg["outbounds"].([]any)
	if !ok {
		outbounds = []any{}
	}

	filtered := make([]any, 0, len(outbounds)+1)
	for _, item := range outbounds {
		obj, ok := item.(map[string]any)
		if ok {
			tag, _ := obj["tag"].(string)
			if tag == outboundTag {
				continue
			}
		}
		filtered = append(filtered, item)
	}

	filtered = append(filtered, gatewayDirectOutbound)
	cfg["outbounds"] = filtered
}

func addGatewayRoutingRule(cfg map[string]any) error {
	routing, ok := cfg["routing"].(map[string]any)
	if !ok {
		return fmt.Errorf("Xray routing section is missing")
	}

	rules, ok := routing["rules"].([]any)
	if !ok {
		rules = []any{}
	}

	filtered := make([]any, 0, len(rules))

	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			filtered = append(filtered, item)
			continue
		}

		inboundTags, _ := rule["inboundTag"].([]any)

		remove := false

		for _, tag := range inboundTags {
			if tag == inboundTag {
				remove = true
				break
			}
		}

		if !remove {
			filtered = append(filtered, item)
		}
	}

	gatewayRule := map[string]any{
		"type": "field",
		"inboundTag": []any{
			inboundTag,
		},
		"outboundTag": outboundTag,
	}

	// Gateway rule must be before generic rules.
	filtered = append([]any{gatewayRule}, filtered...)

	routing["rules"] = filtered
	cfg["routing"] = routing

	return nil
}

func Enable() error {
	cfg, raw, err := loadTemplate()
	if err != nil {
		return err
	}

	addTproxyInbound(cfg)
	addGatewayOutbound(cfg)

	if err := addGatewayRoutingRule(cfg); err != nil {
		return err
	}

	// Create the backup only after all deterministic config mutations and
	// checks have succeeded. Otherwise a validation error would make status
	// report Gateway Mode as enabled even though nothing was saved.
	if err := createBackup(raw); err != nil {
		return err
	}

	if err := saveTemplate(cfg); err != nil {
		// SaveXraySetting validates before persisting. A failed save must not
		// leave a stale backup, because backup existence is the enabled marker.
		if cleanupErr := removeBackup(); cleanupErr != nil {
			return fmt.Errorf("%w; cleanup failed: %v", err, cleanupErr)
		}
		return err
	}

	fmt.Println("Xray Gateway configuration enabled.")

	return nil
}

func IsEnabled() bool {
	_, err := os.Stat(backupPath)
	return err == nil
}

func Disable() error {
	if _, err := os.Stat(backupPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("Gateway Mode is not enabled")
		}
		return fmt.Errorf("check gateway backup: %w", err)
	}

	// `x-ui gateway disable` runs as a fresh CLI process. Unlike Enable(), it
	// previously reached XraySettingService without initializing the database.
	if err := database.InitDB(config.GetDBPath()); err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}

	if err := restoreBackup(); err != nil {
		return err
	}

	if err := removeBackup(); err != nil {
		return err
	}

	fmt.Println("Xray Gateway configuration restored.")

	return nil
}
