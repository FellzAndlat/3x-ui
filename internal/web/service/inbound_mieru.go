package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/mieru"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func validateMieruInbound(inbound *model.Inbound, clients []model.Client) error {
	if inbound.Protocol != model.Mieru {
		return nil
	}
	if err := mieru.ValidatePortBindings(inbound); err != nil {
		return err
	}
	for _, client := range clients {
		if !client.Enable {
			continue
		}
		if strings.TrimSpace(client.Email) == "" || client.Password == "" {
			return fmt.Errorf("Mieru requires a user name and password for every enabled client")
		}
		if len(client.Email) > 64 || len(client.Password) > 64 {
			return fmt.Errorf("Mieru user name and password must be at most 64 bytes")
		}
	}
	return nil
}

func (s *InboundService) DesiredMieruInstances() ([]mieru.Instance, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	if err := db.Model(model.Inbound{}).
		Where("protocol = ? AND enable = ? AND node_id IS NULL", model.Mieru, true).
		Find(&inbounds).Error; err != nil {
		return nil, err
	}
	out := make([]mieru.Instance, 0, len(inbounds))
	if len(inbounds) == 0 {
		return out, nil
	}
	ids := make([]int, 0, len(inbounds))
	for _, ib := range inbounds {
		ids = append(ids, ib.Id)
	}
	var disabledRows []xray.ClientTraffic
	if err := db.Model(xray.ClientTraffic{}).
		Where("inbound_id IN ? AND enable = ?", ids, false).
		Select("inbound_id", "email").Find(&disabledRows).Error; err != nil {
		return nil, err
	}
	disabled := make(map[int]map[string]struct{})
	for _, row := range disabledRows {
		if disabled[row.InboundId] == nil {
			disabled[row.InboundId] = make(map[string]struct{})
		}
		disabled[row.InboundId][row.Email] = struct{}{}
	}
	for _, ib := range inbounds {
		if inst, ok := mieru.InstanceFromInbound(ib); ok {
			if off := disabled[ib.Id]; len(off) > 0 {
				users := inst.Users[:0]
				for _, user := range inst.Users {
					if _, skip := off[user.Name]; !skip {
						users = append(users, user)
					}
				}
				inst.Users = users
			}
			if len(inst.Users) == 0 {
				continue
			}
			out = append(out, inst)
		}
	}
	return out, nil
}

// Rebuild the complete user list after a client change. Mieru reloads it without
// restarting the listener when only credentials have changed.
func (s *InboundService) applyLocalMieru(inboundID int) {
	inbound, err := s.GetInbound(inboundID)
	if err != nil || inbound == nil || inbound.Protocol != model.Mieru || inbound.NodeID != nil {
		return
	}
	rt, err := s.runtimeFor(inbound)
	if err != nil {
		logger.Warning("mieru: runtime lookup failed:", err)
		return
	}
	payload := inbound
	if inbound.Enable {
		payload, err = s.buildInboundForLocalRuntime(database.GetDB(), inbound)
		if err != nil {
			logger.Warning("mieru: client list rebuild failed:", err)
			return
		}
	}
	if err := rt.UpdateInbound(context.Background(), inbound, payload); err != nil {
		logger.Warning("mieru: immediate client apply failed for inbound", inboundID, ":", err)
	}
}
