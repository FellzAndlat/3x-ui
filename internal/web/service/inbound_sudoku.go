package service

import (
    "context"
    "encoding/json"
    "errors"
    "os"
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
    FallbackAddress    string                `json:"fallbackAddress"`
    Key                string                `json:"key"`
    AEAD               string                `json:"aead"`
    SuspiciousAction   string                `json:"suspiciousAction"`
    PaddingMin         int                   `json:"paddingMin"`
    PaddingMax         int                   `json:"paddingMax"`
    ASCII              string                `json:"ascii"`
    CustomTable        string                `json:"customTable"`
    CustomTables       []string              `json:"customTables"`
    EnablePureDownlink bool                  `json:"enablePureDownlink"`
    Multiplex          string                `json:"multiplex"`
    HTTPMask           sudokuHTTPMaskSettings `json:"httpmask"`
    Clients            []model.Client        `json:"clients"`
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
    if settings.Multiplex == "" {
        settings.Multiplex = "off"
    }
    if settings.HTTPMask.Multiplex != "" {
        settings.Multiplex = settings.HTTPMask.Multiplex
        settings.HTTPMask.Multiplex = ""
    }
    if settings.HTTPMask.Mode == "" {
        settings.HTTPMask.Mode = "auto"
    }
    return settings, nil
}

func EnsureSudokuCredentials(inboundID int) error {
    sudokuCredentialsMu.Lock()
    defer sudokuCredentialsMu.Unlock()

    db := database.GetDB()
    var inbound model.Inbound
    if err := db.First(&inbound, inboundID).Error; err != nil {
        return err
    }
    if inbound.Protocol != model.Sudoku {
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

    changed := false
    if !sudoku.ValidPrivateKey(masterPrivate) {
        publicKey, privateKey, keyErr := sudoku.GenerateMasterKey(ctx, binary)
        if keyErr != nil {
            return keyErr
        }
        settings.Key = publicKey
        masterPrivate = privateKey
        if err := sudoku.WriteMasterKey(binDir, inboundID, masterPrivate); err != nil {
            return err
        }
        changed = true
    } else if strings.TrimSpace(settings.Key) == "" {
        // The public part is intentionally rotated rather than re-implementing
        // Sudoku's Edwards25519 key math inside the panel.
        publicKey, privateKey, keyErr := sudoku.GenerateMasterKey(ctx, binary)
        if keyErr != nil {
            return keyErr
        }
        settings.Key = publicKey
        masterPrivate = privateKey
        if err := sudoku.WriteMasterKey(binDir, inboundID, masterPrivate); err != nil {
            return err
        }
        for i := range settings.Clients {
            splitKey, keyErr := sudoku.GenerateSplitKey(ctx, binary, masterPrivate)
            if keyErr != nil {
                return keyErr
            }
            settings.Clients[i].SudokuPrivateKey = splitKey
        }
        changed = true
    }

    for i := range settings.Clients {
        if sudoku.ValidPrivateKey(settings.Clients[i].SudokuPrivateKey) {
            continue
        }
        splitKey, keyErr := sudoku.GenerateSplitKey(ctx, binary, masterPrivate)
        if keyErr != nil {
            return keyErr
        }
        settings.Clients[i].SudokuPrivateKey = splitKey
        changed = true
    }

    if changed {
        data, marshalErr := json.MarshalIndent(settings, "", "  ")
        if marshalErr != nil {
            return marshalErr
        }
        if err := db.Model(&model.Inbound{}).
            Where("id = ?", inboundID).
            Update("settings", string(data)).Error; err != nil {
            return err
        }
    }
    return nil
}

func DesiredSudokuInstances() ([]sudoku.Instance, error) {
    if err := DisableUnavailableSudokuInbounds(); err != nil {
        return nil, err
    }
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
                    Disable: settings.HTTPMask.Disable,
                    Mode: settings.HTTPMask.Mode,
                    TLS: settings.HTTPMask.TLS,
                    Host: settings.HTTPMask.Host,
                    PathRoot: settings.HTTPMask.PathRoot,
                },
            },
        })
    }
    return desired, nil
}

func RefreshSudokuCredentialsOnInbound(inbound *model.Inbound) error {
    if inbound == nil || inbound.Protocol != model.Sudoku {
        return nil
    }
    return EnsureSudokuCredentials(inbound.Id)
}
