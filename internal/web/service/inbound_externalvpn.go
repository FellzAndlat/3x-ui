package service

import (
	"fmt"
	"encoding/json"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
	"github.com/SawaMEN/3x-ui/v3/internal/snell"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func prepareExternalVPN(ib *model.Inbound, previous string) error {
	if err := snell.Prepare(ib, previous); err != nil {
		return err
	}
	if ib.Protocol == model.FPTN || ib.Protocol == model.OpenFlux {
		return externalvpn.PrepareAdditional(ib, previous)
	}
	if !externalvpn.IsManaged(ib.Protocol) {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(ib.Settings), &raw); err != nil {
		return err
	}
	if raw == nil {
		raw = map[string]any{}
	}
	if ib.Protocol == model.Pingtunnel {
		ib.Port = 0
		var old externalvpn.Settings
		_ = json.Unmarshal([]byte(previous), &old)
		key, _ := raw["key"].(float64)
		if key == 0 {
			if old.Key == 0 {
				generated, err := externalvpn.GeneratePingtunnelKey()
				if err != nil {
					return err
				}
				old.Key = generated
			}
			raw["key"] = old.Key
		}
		if raw["encrypt"] == nil {
			raw["encrypt"] = "chacha20"
		}
		secret, _ := raw["encryptKey"].(string)
		if secret == "" && raw["encrypt"] != "" {
			secret = old.EncryptKey
			if secret == "" {
				generated, err := externalvpn.GenerateSecret()
				if err != nil {
					return err
				}
				secret = generated
			}
			raw["encryptKey"] = secret
		}
	} else {
		if raw["hostname"] == nil || raw["hostname"] == "" {
			raw["hostname"] = "trusttunnel.local"
		}
		if clients, ok := raw["clients"].([]any); ok {
			for _, entry := range clients {
				client, ok := entry.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid TrustTunnel client")
				}
				if client["password"] == nil || client["password"] == "" {
					secret, err := externalvpn.GenerateSecret()
					if err != nil {
						return err
					}
					client["password"] = secret
				}
			}
		}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	ib.Settings = string(data)
	_, err = externalvpn.FromInbound(ib)
	return err
}

func (s *InboundService) DesiredExternalVPNInstances() ([]externalvpn.Instance, error) {
	var rows []*model.Inbound
	err := database.GetDB().Where("protocol IN ? AND enable = ? AND node_id IS NULL", []model.Protocol{model.Pingtunnel, model.TrustTunnel, model.FPTN, model.OpenFlux}, true).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	disabled := map[int]map[string]bool{}
	if len(ids) > 0 {
		var stats []xray.ClientTraffic
		if err := database.GetDB().Where("inbound_id IN ? AND enable = ?", ids, false).Find(&stats).Error; err != nil {
			return nil, err
		}
		for _, stat := range stats {
			if disabled[stat.InboundId] == nil {
				disabled[stat.InboundId] = map[string]bool{}
			}
			disabled[stat.InboundId][stat.Email] = true
		}
	}
	out := make([]externalvpn.Instance, 0, len(rows))
	for _, row := range rows {
		inst, err := externalvpn.FromInbound(row)
		if err != nil {
			return nil, fmt.Errorf("inbound %d: %w", row.Id, err)
		}
		for i := range inst.Settings.Clients {
			if disabled[row.Id][inst.Settings.Clients[i].Email] {
				inst.Settings.Clients[i].Enable = false
			}
		}
		out = append(out, inst)
	}
	return out, nil
}

func validateAdditionalVPNClients(ib *model.Inbound) error {
	if ib.Protocol != model.FPTN && ib.Protocol != model.OpenFlux {
		return nil
	}
	_, err := externalvpn.FromInbound(ib)
	return err
}
