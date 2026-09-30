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
    inboundPort = 52345
)

var tproxyInbound = map[string]any{
    "listen":   "127.0.0.1",
    "port":     inboundPort,
    "protocol": "dokodemo-door",
    "settings": map[string]any{
        "followRedirect": true,
        "network":       "tcp,udp",
    },
    "sniffing": map[string]any{
        "destOverride": []any{
            "http",
            "tls",
            "quic",
        },
        "enabled":  true,
        "routeOnly": true,
    },
    "streamSettings": map[string]any{
        "sockopt": map[string]any{
            "tproxy": "tproxy",
        },
    },
    "tag": inboundTag,
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
    }

    if err := os.WriteFile(backupPath, []byte(raw), 0600); err != nil {
        return fmt.Errorf("write gateway backup: %w", err)
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
    filtered := make([]any, 0, len(inbounds))

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
        "outboundTag": "direct",
    }

    // Gateway rule must be before generic rules.
    filtered = append([]any{gatewayRule}, filtered...)

    routing["rules"] = filtered
    cfg["routing"] = routing

    return nil
}

func modifyDirectOutbound(cfg map[string]any) error {
    outbounds, ok := cfg["outbounds"].([]any)
    if !ok {
        return fmt.Errorf("Xray outbounds section is missing")
    }

    found := false

    for _, item := range outbounds {
        outbound, ok := item.(map[string]any)
        if !ok {
            continue
        }

        tag, _ := outbound["tag"].(string)

        if tag != "direct" {
            continue
        }

        found = true

        settings, ok := outbound["settings"].(map[string]any)
        if !ok {
            settings = map[string]any{}
            outbound["settings"] = settings
        }

        settings["finalRules"] = []any{
            map[string]any{
                "action": "allow",
            },
        }
    }

    if !found {
        return fmt.Errorf("outbound with tag \"direct\" was not found")
    }

    return nil
}

func Enable() error {
    cfg, raw, err := loadTemplate()
    if err != nil {
        return err
    }

    if err := createBackup(raw); err != nil {
        return err
    }

    addTproxyInbound(cfg)

    if err := addGatewayRoutingRule(cfg); err != nil {
        return err
    }

    if err := modifyDirectOutbound(cfg); err != nil {
        return err
    }

    if err := saveTemplate(cfg); err != nil {
        // Если сохранение не удалось, backup всё равно остаётся.
        // Gateway CLI сможет восстановить исходный конфиг.
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
        return fmt.Errorf("Gateway Mode is not enabled")
    }

    if err := restoreBackup(); err != nil {
        return err
    }

    if err := os.Remove(backupPath); err != nil {
        return fmt.Errorf("remove gateway backup: %w", err)
    }

    fmt.Println("Xray Gateway configuration restored.")

    return nil
}
