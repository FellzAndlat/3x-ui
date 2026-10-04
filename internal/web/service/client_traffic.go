package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/util/common"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"

	"gorm.io/gorm"
)

func (s *ClientService) ResetTrafficByEmail(inboundSvc *InboundService, email string) (bool, error) {
	if email == "" {
		return false, common.NewError("client email is required")
	}
	if _, err := s.GetRecordByEmail(nil, email); err != nil {
		return false, err
	}
	_, needRestart, err := s.resetClientTrafficSet(inboundSvc, func(*gorm.DB) ([]string, error) {
		return []string{email}, nil
	}, nil)
	return needRestart, err
}

func (s *ClientService) BulkResetTraffic(inboundSvc *InboundService, emails []string) (int, error) {
	cleanEmails := trimmedUniqueEmails(emails)
	if len(cleanEmails) == 0 {
		return 0, nil
	}
	affected, _, err := s.resetClientTrafficSet(inboundSvc, func(*gorm.DB) ([]string, error) {
		return cleanEmails, nil
	}, nil)
	return affected, err
}

func (s *ClientService) ResetAllClientTraffics(inboundSvc *InboundService, id int) error {
	_, _, err := s.resetClientTrafficSet(inboundSvc, func(tx *gorm.DB) ([]string, error) {
		var emails []string
		if id == -1 {
			err := tx.Model(&xray.ClientTraffic{}).Pluck("email", &emails).Error
			return emails, err
		}
		// A shared traffic row may retain a deleted inbound's ID. Membership
		// comes from the normalized join, never client_traffics.inbound_id.
		err := tx.Table("client_inbounds ci").Select("c.email").
			Joins("JOIN clients c ON c.id = ci.client_id").Where("ci.inbound_id = ?", id).
			Pluck("c.email", &emails).Error
		return emails, err
	}, &id)
	return err
}

func (s *ClientService) ResetAllTraffics() (bool, error) {
	affected, needRestart, err := s.resetClientTrafficSet(&InboundService{}, func(tx *gorm.DB) ([]string, error) {
		var emails []string
		err := tx.Model(&xray.ClientTraffic{}).Pluck("email", &emails).Error
		return emails, err
	}, nil)
	return affected > 0 || needRestart, err
}

type clientTrafficResetPlan struct {
	inbound model.Inbound
	emails  []string
}

// All reset entry points share one atomic accounting/membership update. Runtime
// I/O is deliberately outside the serial writer, so a slow node cannot stall
// traffic polling. A failed node keeps its old receipt baseline: its next poll
// must not add all of its pre-reset usage back to the central counter.
func (s *ClientService) resetClientTrafficSet(inboundSvc *InboundService, selectEmails func(*gorm.DB) ([]string, error), resetInboundID *int) (int, bool, error) {
	var emails []string
	var plans []clientTrafficResetPlan
	var affected int
	var needRestart bool
	err := runSerializedTx(func(tx *gorm.DB) error {
		var err error
		emails, err = selectEmails(tx)
		if err != nil {
			return err
		}
		emails = trimmedUniqueEmails(emails)
		if len(emails) == 0 {
			return nil
		}
		plans, needRestart, err = reenableResetClientsTx(tx, emails)
		if err != nil {
			return err
		}
		if err := adjustGroupBaselinesForRemovedTraffic(tx, emails); err != nil {
			return err
		}
		remoteNodes := make(map[int]struct{})
		for _, plan := range plans {
			if plan.inbound.NodeID != nil {
				remoteNodes[*plan.inbound.NodeID] = struct{}{}
			}
		}
		for _, batch := range chunkStrings(emails, sqlInChunk) {
			var disabled int64
			if err := tx.Model(&xray.ClientTraffic{}).Where("email IN ? AND enable = ?", batch, false).Count(&disabled).Error; err != nil {
				return err
			}
			needRestart = needRestart || disabled > 0
			res := tx.Model(&xray.ClientTraffic{}).Where("email IN ?", batch).
				Updates(map[string]any{"enable": true, "up": 0, "down": 0})
			if res.Error != nil {
				return res.Error
			}
			affected += int(res.RowsAffected)
			var receipts []model.NodeClientTraffic
			if err := tx.Where("email IN ?", batch).Find(&receipts).Error; err != nil {
				return err
			}
			var removeIDs []int
			for _, receipt := range receipts {
				if _, remote := remoteNodes[receipt.NodeId]; !remote {
					removeIDs = append(removeIDs, receipt.Id)
				}
			}
			for _, ids := range chunkInts(removeIDs, sqlInChunk) {
				if err := tx.Where("id IN ?", ids).Delete(&model.NodeClientTraffic{}).Error; err != nil {
					return err
				}
			}
		}
		if err := clearGlobalTraffic(tx, emails...); err != nil {
			return err
		}
		if resetInboundID != nil {
			query := tx.Model(&model.Inbound{})
			if *resetInboundID == -1 {
				query = query.Where("id > ?", -1)
			} else {
				query = query.Where("id = ?", *resetInboundID)
			}
			if err := query.Update("last_traffic_reset_time", time.Now().UnixMilli()).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	if needRestart {
		coreType, _ := (&SettingService{}).GetCoreType()
		if coreType == CoreTypeSingBox {
			(&SingBoxService{}).SetToNeedRestart()
		} else {
			(&XrayService{}).SetToNeedRestart()
		}
	}
	for _, email := range emails {
		inboundSvc.resetMtprotoClientQuota(email)
	}

	// The node endpoint resets this email on every node inbound, so dispatch
	// exactly once per node/email when a client spans several of its inbounds.
	type nodeEmail struct {
		node  int
		email string
	}
	seen := make(map[nodeEmail]struct{})
	var applies []inboundApply
	for _, plan := range plans {
		if plan.inbound.NodeID == nil {
			continue
		}
		for _, email := range plan.emails {
			key := nodeEmail{*plan.inbound.NodeID, email}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			applies = append(applies, inboundApply{id: plan.inbound.Id, run: func() (bool, error) {
				rt, err := inboundSvc.runtimeFor(&plan.inbound)
				if err != nil {
					return false, err
				}
				ctx, cancel := nodePushContext()
				defer cancel()
				if err := rt.ResetClientTraffic(ctx, &plan.inbound, email); err != nil {
					return false, err
				}
				err = submitTrafficWrite(func() error {
					return database.GetDB().Where("node_id = ? AND email = ?", key.node, email).Delete(&model.NodeClientTraffic{}).Error
				})
				return false, err
			}})
		}
	}
	_, err = fanoutInboundApplies(applies)
	return affected, needRestart, err
}

// Re-enable all three stored projections together. This also avoids one full
// ClientService.Update and one settings rewrite per client in a bulk reset.
func reenableResetClientsTx(tx *gorm.DB, emails []string) ([]clientTrafficResetPlan, bool, error) {
	type target struct {
		InboundID int
		Email     string
	}
	byInbound := make(map[int][]string)
	needRestart := false
	now := time.Now().UnixMilli()
	for _, batch := range chunkStrings(emails, sqlInChunk) {
		res := tx.Model(&model.ClientRecord{}).Where("email IN ? AND enable = ?", batch, false).
			Updates(map[string]any{"enable": true, "auto_disabled_by_core": "", "updated_at": now})
		if res.Error != nil {
			return nil, false, res.Error
		}
		needRestart = needRestart || res.RowsAffected > 0
		var targets []target
		if err := tx.Table("client_inbounds ci").Select("ci.inbound_id, c.email").
			Joins("JOIN clients c ON c.id = ci.client_id").Where("c.email IN ?", batch).Scan(&targets).Error; err != nil {
			return nil, false, err
		}
		for _, target := range targets {
			byInbound[target.InboundID] = append(byInbound[target.InboundID], target.Email)
		}
	}
	ids := make([]int, 0, len(byInbound))
	for id := range byInbound {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	var inbounds []model.Inbound
	for _, batch := range chunkInts(ids, sqlInChunk) {
		var part []model.Inbound
		if err := tx.Where("id IN ?", batch).Find(&part).Error; err != nil {
			return nil, false, err
		}
		inbounds = append(inbounds, part...)
	}
	plans := make([]clientTrafficResetPlan, 0, len(inbounds))
	for _, ib := range inbounds {
		wanted := make(map[string]struct{}, len(byInbound[ib.Id]))
		for _, email := range byInbound[ib.Id] {
			wanted[email] = struct{}{}
		}
		var settings map[string]any
		if err := json.Unmarshal([]byte(ib.Settings), &settings); err != nil {
			return nil, false, fmt.Errorf("inbound %d settings: %w", ib.Id, err)
		}
		clients, _ := settings["clients"].([]any)
		changed := false
		for _, item := range clients {
			cm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			email, _ := cm["email"].(string)
			if _, selected := wanted[email]; !selected {
				continue
			}
			if enabled, _ := cm["enable"].(bool); enabled {
				continue
			}
			cm["enable"], cm["updated_at"] = true, now
			changed = true
		}
		if changed {
			raw, err := json.Marshal(settings)
			if err != nil {
				return nil, false, err
			}
			ib.Settings = string(raw)
			if err := tx.Model(&model.Inbound{}).Where("id = ?", ib.Id).UpdateColumn("settings", ib.Settings).Error; err != nil {
				return nil, false, err
			}
			needRestart = true
			if ib.NodeID != nil {
				if err := (&NodeService{}).MarkNodeDirtyTx(tx, *ib.NodeID); err != nil {
					return nil, false, err
				}
			}
		}
		plans = append(plans, clientTrafficResetPlan{inbound: ib, emails: byInbound[ib.Id]})
	}
	return plans, needRestart, nil
}
