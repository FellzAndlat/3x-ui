package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
	"gorm.io/gorm"
)

type coreCompatibilityInbound struct {
	Id       int
	Protocol model.Protocol
	Settings string
}

// SyncClientCoreCompatibility synchronizes client enable state with a core switch.
// A client is incompatible when any local inbound it is attached to is unsupported
// by the target core. AutoDisabledByCore distinguishes this from a manual disable.
func SyncClientCoreCompatibility(oldCore, newCore string) error {
	if oldCore == newCore {
		return nil
	}
	if !isSupportedCoreType(oldCore) || !isSupportedCoreType(newCore) {
		return nil
	}

	db := database.GetDB()
	var inbounds []coreCompatibilityInbound
	if err := db.Model(&model.Inbound{}).
		Select("id, protocol, settings").
		Where("node_id IS NULL").
		Find(&inbounds).Error; err != nil {
		return err
	}

	localIDs := make(map[int]struct{}, len(inbounds))
	protocolByInbound := make(map[int]model.Protocol, len(inbounds))
	for _, ib := range inbounds {
		localIDs[ib.Id] = struct{}{}
		protocolByInbound[ib.Id] = ib.Protocol
	}

	var links []model.ClientInbound
	if err := db.Find(&links).Error; err != nil {
		return err
	}

	protocolsByClient := make(map[int]map[model.Protocol]struct{})
	for _, link := range links {
		if _, ok := localIDs[link.InboundId]; !ok {
			continue
		}
		if protocolsByClient[link.ClientId] == nil {
			protocolsByClient[link.ClientId] = make(map[model.Protocol]struct{})
		}
		protocolsByClient[link.ClientId][protocolByInbound[link.InboundId]] = struct{}{}
	}
	if len(protocolsByClient) == 0 {
		return nil
	}

	ids := make([]int, 0, len(protocolsByClient))
	for id := range protocolsByClient {
		ids = append(ids, id)
	}
	var clients []model.ClientRecord
	if err := db.Where("id IN ?", ids).Find(&clients).Error; err != nil {
		return err
	}

	changes := make(map[string]bool)
	return db.Transaction(func(tx *gorm.DB) error {
		for i := range clients {
			record := &clients[i]
			incompatible := false
			for protocol := range protocolsByClient[record.Id] {
				if !coreSupportsInboundProtocol(newCore, protocol) {
					incompatible = true
					break
				}
			}

			wantEnable := record.Enable
			autoCore := record.AutoDisabledByCore
			switch {
			case incompatible:
				if record.Enable {
					wantEnable = false
					autoCore = newCore
				} else if record.AutoDisabledByCore != "" {
					autoCore = newCore
				}
			case record.AutoDisabledByCore != "":
				wantEnable = true
				autoCore = ""
			}

			if wantEnable == record.Enable && autoCore == record.AutoDisabledByCore {
				continue
			}

			if err := tx.Model(&model.ClientRecord{}).
				Where("id = ?", record.Id).
				Updates(map[string]any{
					"enable":                wantEnable,
					"auto_disabled_by_core": autoCore,
					"updated_at":            time.Now().UnixMilli(),
				}).Error; err != nil {
				return err
			}
			if err := tx.Model(&xray.ClientTraffic{}).
				Where("email = ?", record.Email).
				Update("enable", wantEnable).Error; err != nil {
				return err
			}
			changes[record.Email] = wantEnable
		}

		if len(changes) == 0 {
			return nil
		}
		for _, ib := range inbounds {
			if strings.TrimSpace(ib.Settings) == "" {
				continue
			}
			var settings map[string]any
			if err := json.Unmarshal([]byte(ib.Settings), &settings); err != nil {
				continue
			}
			rawClients, _ := settings["clients"].([]any)
			changed := false
			for idx, raw := range rawClients {
				client, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				email, _ := client["email"].(string)
				email = strings.TrimSpace(email)
				enable, ok := changes[email]
				if !ok {
					continue
				}
				if current, ok := client["enable"].(bool); !ok || current != enable {
					client["enable"] = enable
					rawClients[idx] = client
					changed = true
				}
			}
			if !changed {
				continue
			}
			settings["clients"] = rawClients
			data, err := json.MarshalIndent(settings, "", "  ")
			if err != nil {
				return err
			}
			if err := tx.Model(&model.Inbound{}).Where("id = ?", ib.Id).Update("settings", string(data)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func isSupportedCoreType(core string) bool {
	return core == CoreTypeXray || core == CoreTypeSingBox
}

func coreSupportsInboundProtocol(core string, protocol model.Protocol) bool {
	switch core {
	case CoreTypeXray:
		switch protocol {
		case model.NaiveProxy, model.Snell, model.AnyTLS, model.ShadowTLS:
			return false
		default:
			return true
		}
	case CoreTypeSingBox:
		switch protocol {
		case model.WireGuard, model.Tunnel:
			return false
		default:
			return true
		}
	default:
		return false
	}
}

func clearCoreAutoDisabledByEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	return database.GetDB().Model(&model.ClientRecord{}).
		Where("email = ?", email).
		Update("auto_disabled_by_core", "").Error
}
