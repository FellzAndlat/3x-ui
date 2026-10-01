package gateway

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

const (
	backupPath = "/etc/x-ui/gateway-xray-backup.json"

	inboundTag        = "in-tproxy"
	legacyOutboundTag = "xui-gateway-direct"
	inboundPort       = 52345
)

// State describes both the active Gateway inbound and recovery/legacy markers.
// Enabled remains true while any Gateway-owned artifact exists so the UI always
// offers a cleanup path instead of allowing a second enable over stale state.
type State struct {
	Enabled      bool
	Configured   bool
	BackupExists bool
}

var operationMu sync.Mutex

// InboundPort returns the local port Linux TPROXY rules must redirect to.
func InboundPort() int {
	return inboundPort
}

func gatewayInbound() map[string]any {
	return map[string]any{
		// The default listen address is 0.0.0.0. A transparent gateway must not
		// bind this listener to loopback because redirected LAN traffic arrives
		// through PREROUTING.
		"port":     inboundPort,
		"protocol": "tunnel",
		"settings": map[string]any{
			"allowedNetwork": "tcp,udp",
			"followRedirect": true,
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
}

func decodeXrayTemplate(raw string) (map[string]any, error) {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse Xray template: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("parse Xray template: top-level JSON value must be an object")
	}
	return cfg, nil
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

	cfg, err := decodeXrayTemplate(raw)
	if err != nil {
		return nil, "", err
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

func backupExists() (bool, error) {
	_, err := os.Stat(backupPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("check gateway backup: %w", err)
}

func createBackup(raw string) error {
	file, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf(
				"gateway backup already exists at %s; disable Gateway Mode first",
				backupPath,
			)
		}
		return fmt.Errorf("create gateway backup: %w", err)
	}

	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(backupPath)
	}
	if _, err := file.WriteString(raw); err != nil {
		cleanup()
		return fmt.Errorf("write gateway backup: %w", err)
	}
	if err := file.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync gateway backup: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("close gateway backup: %w", err)
	}
	return nil
}

func removeBackup() error {
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove gateway backup: %w", err)
	}
	return nil
}

func arrayField(cfg map[string]any, key string) ([]any, error) {
	value, exists := cfg[key]
	if !exists || value == nil {
		return []any{}, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("config %s section must be an array", key)
	}
	return items, nil
}

func itemTag(item any) string {
	obj, ok := item.(map[string]any)
	if !ok {
		return ""
	}
	tag, _ := obj["tag"].(string)
	return tag
}

func xrayPort(value any) (int, bool) {
	switch port := value.(type) {
	case int:
		return port, true
	case int32:
		return int(port), true
	case int64:
		return int(port), true
	case float64:
		converted := int(port)
		if port != float64(converted) {
			return 0, false
		}
		return converted, true
	default:
		return 0, false
	}
}

func hasTPROXYSockopt(inbound map[string]any) bool {
	stream, ok := inbound["streamSettings"].(map[string]any)
	if !ok {
		return false
	}
	sockopt, ok := stream["sockopt"].(map[string]any)
	if !ok {
		return false
	}
	return sockopt["tproxy"] == "tproxy"
}

func isXrayGatewayInbound(item any) bool {
	inbound, ok := item.(map[string]any)
	if !ok || itemTag(item) != inboundTag || inbound["protocol"] != "tunnel" {
		return false
	}
	port, portOK := xrayPort(inbound["port"])
	if !portOK || port != inboundPort || !hasTPROXYSockopt(inbound) {
		return false
	}
	settings, ok := inbound["settings"].(map[string]any)
	return ok && settings["allowedNetwork"] == "tcp,udp" && settings["followRedirect"] == true
}

// isLegacyXrayGatewayInbound recognizes the first GUI implementation so an
// upgrade can still clean it up without treating an arbitrary matching tag as
// Gateway-owned.
func isLegacyXrayGatewayInbound(item any) bool {
	inbound, ok := item.(map[string]any)
	if !ok || itemTag(item) != inboundTag || inbound["protocol"] != "dokodemo-door" {
		return false
	}
	if inbound["listen"] != "127.0.0.1" || !hasTPROXYSockopt(inbound) {
		return false
	}
	port, portOK := xrayPort(inbound["port"])
	if !portOK || port != inboundPort {
		return false
	}
	settings, ok := inbound["settings"].(map[string]any)
	return ok && settings["network"] == "tcp,udp" && settings["followRedirect"] == true
}

func isOwnedXrayGatewayInbound(item any) bool {
	return isXrayGatewayInbound(item) || isLegacyXrayGatewayInbound(item)
}

func validateXrayGatewayConflicts(cfg map[string]any) error {
	inbounds, err := arrayField(cfg, "inbounds")
	if err != nil {
		return err
	}
	for _, item := range inbounds {
		if isOwnedXrayGatewayInbound(item) {
			continue
		}
		if itemTag(item) == inboundTag {
			return fmt.Errorf("Xray inbound tag %q is already used by a non-Gateway inbound", inboundTag)
		}
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}
		port, ok := xrayPort(inbound["port"])
		if !ok || port != inboundPort {
			continue
		}
		tag := itemTag(item)
		if tag == "" {
			tag = "<untagged>"
		}
		return fmt.Errorf("Xray inbound %q already uses Gateway port %d", tag, inboundPort)
	}
	return nil
}

func hasInboundTag(rule map[string]any, tag string) bool {
	values, ok := rule["inboundTag"].([]any)
	if !ok {
		return false
	}
	for _, value := range values {
		if value == tag {
			return true
		}
	}
	return false
}

func isLegacyGatewayRule(item any) bool {
	rule, ok := item.(map[string]any)
	if !ok || rule["type"] != "field" || rule["outboundTag"] != legacyOutboundTag {
		return false
	}
	values, ok := rule["inboundTag"].([]any)
	return ok && len(values) == 1 && values[0] == inboundTag
}

func isLegacyGatewayOutbound(item any) bool {
	outbound, ok := item.(map[string]any)
	if !ok || itemTag(item) != legacyOutboundTag || outbound["protocol"] != "freedom" {
		return false
	}
	settings, ok := outbound["settings"].(map[string]any)
	if !ok {
		return false
	}
	finalRules, ok := settings["finalRules"].([]any)
	if !ok || len(finalRules) != 1 {
		return false
	}
	rule, ok := finalRules[0].(map[string]any)
	return ok && len(rule) == 1 && rule["action"] == "allow"
}

func hasGatewayInbound(cfg map[string]any) bool {
	inbounds, ok := cfg["inbounds"].([]any)
	if !ok {
		return false
	}
	for _, item := range inbounds {
		if isOwnedXrayGatewayInbound(item) {
			return true
		}
	}
	return false
}

func hasLegacyGatewayArtifacts(cfg map[string]any) bool {
	if outbounds, ok := cfg["outbounds"].([]any); ok {
		for _, item := range outbounds {
			if isLegacyGatewayOutbound(item) {
				return true
			}
		}
	}
	if routing, ok := cfg["routing"].(map[string]any); ok {
		if rules, ok := routing["rules"].([]any); ok {
			for _, item := range rules {
				if isLegacyGatewayRule(item) {
					return true
				}
			}
		}
	}
	return false
}

func hasGatewayArtifacts(cfg map[string]any) bool {
	return hasGatewayInbound(cfg) || hasLegacyGatewayArtifacts(cfg)
}

func replaceTaggedItem(cfg map[string]any, key, tag string, replacement any) error {
	items, err := arrayField(cfg, key)
	if err != nil {
		return err
	}
	filtered := make([]any, 0, len(items)+1)
	for _, item := range items {
		if itemTag(item) != tag {
			filtered = append(filtered, item)
		}
	}
	cfg[key] = append(filtered, replacement)
	return nil
}

func applyGatewayConfig(cfg map[string]any) error {
	// Gateway Mode must not select an outbound for the operator. Transparent
	// traffic enters the normal Xray dispatcher and therefore follows the same
	// routing rules/default outbound as any other inbound.
	if err := validateXrayGatewayConflicts(cfg); err != nil {
		return err
	}
	return replaceTaggedItem(cfg, "inbounds", inboundTag, gatewayInbound())
}

func removeXrayGatewayInbound(cfg map[string]any) (bool, error) {
	inbounds, err := arrayField(cfg, "inbounds")
	if err != nil {
		return false, err
	}
	filtered := make([]any, 0, len(inbounds))
	removed := false
	for _, item := range inbounds {
		if isOwnedXrayGatewayInbound(item) {
			removed = true
			continue
		}
		filtered = append(filtered, item)
	}
	if removed {
		cfg["inbounds"] = filtered
	}
	return removed, nil
}

func removeLegacyGatewayOutbound(cfg map[string]any) (bool, error) {
	outbounds, err := arrayField(cfg, "outbounds")
	if err != nil {
		return false, err
	}
	filtered := make([]any, 0, len(outbounds))
	removed := false
	for _, item := range outbounds {
		if isLegacyGatewayOutbound(item) {
			removed = true
			continue
		}
		filtered = append(filtered, item)
	}
	if removed {
		cfg["outbounds"] = filtered
	}
	return removed, nil
}

func removeLegacyGatewayRoutingRule(cfg map[string]any) (bool, error) {
	value, exists := cfg["routing"]
	if !exists || value == nil {
		return false, nil
	}
	routing, ok := value.(map[string]any)
	if !ok {
		return false, fmt.Errorf("Xray routing section must be an object")
	}
	value, exists = routing["rules"]
	if !exists || value == nil {
		return false, nil
	}
	rules, ok := value.([]any)
	if !ok {
		return false, fmt.Errorf("Xray routing rules must be an array")
	}
	filtered := make([]any, 0, len(rules))
	removed := false
	for _, item := range rules {
		if isLegacyGatewayRule(item) {
			removed = true
			continue
		}
		filtered = append(filtered, item)
	}
	if removed {
		routing["rules"] = filtered
		cfg["routing"] = routing
	}
	return removed, nil
}

func removeGatewayConfig(cfg map[string]any) (bool, error) {
	removed := false

	changed, err := removeXrayGatewayInbound(cfg)
	if err != nil {
		return false, err
	}
	removed = removed || changed

	// Clean up exact objects created by the first Gateway GUI implementation.
	// Matching their complete owned shape avoids deleting user objects that only
	// happen to reuse an old internal tag.
	changed, err = removeLegacyGatewayOutbound(cfg)
	if err != nil {
		return false, err
	}
	removed = removed || changed

	changed, err = removeLegacyGatewayRoutingRule(cfg)
	if err != nil {
		return false, err
	}
	return removed || changed, nil
}

func getStateUnlocked() (State, error) {
	cfg, _, err := loadTemplate()
	if err != nil {
		return State{}, err
	}
	backup, err := backupExists()
	if err != nil {
		return State{}, err
	}
	configured := hasGatewayInbound(cfg)
	return State{
		Enabled:      hasGatewayArtifacts(cfg) || backup,
		Configured:   configured,
		BackupExists: backup,
	}, nil
}

func GetState() (State, error) {
	operationMu.Lock()
	defer operationMu.Unlock()
	return getStateUnlocked()
}

func IsEnabled() bool {
	state, err := GetState()
	if err == nil {
		return state.Enabled
	}
	// Preserve the old marker semantics if the database is temporarily
	// unavailable. This keeps the CLI/UI from offering a second enable that
	// could overwrite recovery data.
	exists, statErr := backupExists()
	return statErr == nil && exists
}

func Enable() error {
	operationMu.Lock()
	defer operationMu.Unlock()

	cfg, raw, err := loadTemplate()
	if err != nil {
		return err
	}
	backup, err := backupExists()
	if err != nil {
		return err
	}
	if hasGatewayArtifacts(cfg) || backup {
		return fmt.Errorf("Gateway Mode is already enabled or requires cleanup")
	}

	if err := applyGatewayConfig(cfg); err != nil {
		return err
	}
	if err := createBackup(raw); err != nil {
		return err
	}
	if err := saveTemplate(cfg); err != nil {
		if cleanupErr := removeBackup(); cleanupErr != nil {
			return fmt.Errorf("%w; cleanup failed: %v", err, cleanupErr)
		}
		return err
	}

	fmt.Println("Xray Gateway configuration enabled.")
	return nil
}

func Disable() error {
	operationMu.Lock()
	defer operationMu.Unlock()

	cfg, _, err := loadTemplate()
	if err != nil {
		return err
	}
	backup, err := backupExists()
	if err != nil {
		return err
	}
	configured := hasGatewayArtifacts(cfg)
	if !configured && !backup {
		return fmt.Errorf("Gateway Mode is not enabled")
	}

	if configured {
		changed, err := removeGatewayConfig(cfg)
		if err != nil {
			return err
		}
		if changed {
			// Do not restore the whole pre-Gateway snapshot here. Operators may
			// legitimately edit Xray settings while Gateway Mode is enabled; only
			// Gateway-owned objects are removed so those edits are preserved.
			if err := saveTemplate(cfg); err != nil {
				return err
			}
		}
	}

	if backup {
		if err := removeBackup(); err != nil {
			return err
		}
	}

	fmt.Println("Xray Gateway configuration disabled.")
	return nil
}
