package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/util/common"

	"github.com/SawaMEN/3x-ui/v3/internal/xray"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GroupSummary struct {
	Name        string `json:"name"`
	ClientCount int    `json:"clientCount"`
	TrafficUsed int64  `json:"trafficUsed"`
	Up          int64  `json:"up"`
	Down        int64  `json:"down"`
}

func (s *ClientService) ListGroups() ([]GroupSummary, error) {
	db := database.GetDB()
	// email is unique in both clients and client_traffics, so the LEFT JOIN
	// never double-counts a client's traffic.
	var derived []GroupSummary
	if err := db.Table("clients AS c").
		Select("c.group_name AS name, COUNT(*) AS client_count, COALESCE(SUM(ct.up + ct.down), 0) AS traffic_used, COALESCE(SUM(ct.up), 0) AS up, COALESCE(SUM(ct.down), 0) AS down").
		Joins("LEFT JOIN client_traffics ct ON ct.email = c.email").
		Where("c.group_name <> ''").
		Group("c.group_name").
		Scan(&derived).Error; err != nil {
		return nil, err
	}
	var stored []model.ClientGroup
	if err := db.Find(&stored).Error; err != nil {
		return nil, err
	}
	type groupAgg struct {
		count int
		up    int64
		down  int64
	}
	baseUp := make(map[string]int64, len(stored))
	baseDown := make(map[string]int64, len(stored))
	merged := make(map[string]groupAgg, len(derived)+len(stored))
	for _, g := range stored {
		merged[g.Name] = groupAgg{}
		baseUp[g.Name] = g.ResetUp
		baseDown[g.Name] = g.ResetDown
	}
	for _, g := range derived {
		merged[g.Name] = groupAgg{count: g.ClientCount, up: g.Up, down: g.Down}
	}
	out := make([]GroupSummary, 0, len(merged))
	for name, agg := range merged {
		up := max(agg.up-baseUp[name], 0)
		down := max(agg.down-baseDown[name], 0)
		out = append(out, GroupSummary{Name: name, ClientCount: agg.count, TrafficUsed: up + down, Up: up, Down: down})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// adjustGroupBaselinesForRemovedTraffic shifts group baselines down by the clients'
// current counters so ListGroups totals survive a traffic reset or client delete (#5675).
func adjustGroupBaselinesForRemovedTraffic(tx *gorm.DB, emails []string) error {
	if len(emails) == 0 {
		return nil
	}
	type groupDelta struct {
		Name string
		Up   int64
		Down int64
	}
	totals := make(map[string]*groupDelta)
	for _, batch := range chunkStrings(emails, sqlInChunk) {
		var part []groupDelta
		if err := tx.Table("clients AS c").
			Select("c.group_name AS name, COALESCE(SUM(ct.up), 0) AS up, COALESCE(SUM(ct.down), 0) AS down").
			Joins("JOIN client_traffics ct ON ct.email = c.email").
			Where("c.group_name <> '' AND c.email IN ?", batch).
			Group("c.group_name").
			Scan(&part).Error; err != nil {
			return err
		}
		for i := range part {
			if agg, ok := totals[part[i].Name]; ok {
				agg.Up += part[i].Up
				agg.Down += part[i].Down
			} else {
				totals[part[i].Name] = &part[i]
			}
		}
	}
	for name, d := range totals {
		if d.Up == 0 && d.Down == 0 {
			continue
		}
		res := tx.Model(&model.ClientGroup{}).Where("name = ?", name).Updates(map[string]any{
			"reset_up":   gorm.Expr("reset_up - ?", d.Up),
			"reset_down": gorm.Expr("reset_down - ?", d.Down),
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Create(&model.ClientGroup{Name: name, ResetUp: -d.Up, ResetDown: -d.Down}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *ClientService) EmailsByGroup(name string) ([]string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return []string{}, nil
	}
	db := database.GetDB()
	var emails []string
	if err := db.Model(&model.ClientRecord{}).
		Where("group_name = ?", name).
		Order("email ASC").
		Pluck("email", &emails).Error; err != nil {
		return nil, err
	}
	if emails == nil {
		emails = []string{}
	}
	return emails, nil
}

func (s *ClientService) ResetGroupTraffic(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return common.NewError("group name is required")
	}
	return runSerializedTx(func(tx *gorm.DB) error {
		var agg struct{ Up, Down int64 }
		if err := tx.Table("clients AS c").
			Select("COALESCE(SUM(ct.up), 0) AS up, COALESCE(SUM(ct.down), 0) AS down").
			Joins("LEFT JOIN client_traffics ct ON ct.email = c.email").
			Where("c.group_name = ?", name).Scan(&agg).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "name"}},
			DoUpdates: clause.AssignmentColumns([]string{"reset_up", "reset_down"}),
		}).Create(&model.ClientGroup{Name: name, ResetUp: agg.Up, ResetDown: agg.Down}).Error
	})
}

func groupExistsTx(tx *gorm.DB, name string) (bool, error) {
	var count int64
	if err := tx.Model(&model.ClientGroup{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	if err := tx.Model(&model.ClientRecord{}).Where("group_name = ?", name).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *ClientService) CreateGroup(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return common.NewError("group name is required")
	}
	return runSerializedTx(func(tx *gorm.DB) error {
		exists, err := groupExistsTx(tx, name)
		if err != nil {
			return err
		}
		if exists {
			return common.NewError("group already exists")
		}
		return tx.Create(&model.ClientGroup{Name: name}).Error
	})
}

func (s *ClientService) RenameGroup(oldName, newName string) (int, error) {
	oldName, newName = strings.TrimSpace(oldName), strings.TrimSpace(newName)
	if oldName == "" {
		return 0, common.NewError("old group name is required")
	}
	if newName == "" {
		return 0, common.NewError("new group name is required")
	}
	if oldName == newName {
		return 0, nil
	}
	return s.replaceGroupValue(oldName, newName)
}

func (s *ClientService) DeleteGroup(name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, common.NewError("group name is required")
	}
	return s.replaceGroupValue(name, "")
}

func (s *ClientService) RemoveFromGroup(emails []string) (int, error) {
	return s.AddToGroup(emails, "")
}

func (s *ClientService) AddToGroup(emails []string, group string) (int, error) {
	group = strings.TrimSpace(group)
	emails = trimmedUniqueEmails(emails)
	if len(emails) == 0 {
		return 0, nil
	}
	var affected int
	err := runSerializedTx(func(tx *gorm.DB) error {
		var records []model.ClientRecord
		for _, batch := range chunkStrings(emails, sqlInChunk) {
			var rows []model.ClientRecord
			if err := tx.Where("email IN ?", batch).
				Where("group_name IS NULL OR group_name <> ?", group).Find(&rows).Error; err != nil {
				return err
			}
			records = append(records, rows...)
		}
		if len(records) == 0 {
			return nil
		}
		changed := make([]string, 0, len(records))
		for _, rec := range records {
			changed = append(changed, rec.Email)
		}
		// Historical usage stays with the old group. The destination starts
		// counting these clients from the moment they join, even after resets.
		if err := adjustGroupBaselinesForRemovedTraffic(tx, changed); err != nil {
			return err
		}
		if group != "" {
			var up, down int64
			for _, batch := range chunkStrings(changed, sqlInChunk) {
				var totals struct{ Up, Down int64 }
				if err := tx.Model(&xray.ClientTraffic{}).
					Select("COALESCE(SUM(up), 0) AS up, COALESCE(SUM(down), 0) AS down").
					Where("email IN ?", batch).Scan(&totals).Error; err != nil {
					return err
				}
				up, down = up+totals.Up, down+totals.Down
			}
			if err := shiftGroupBaseline(tx, group, up, down); err != nil {
				return err
			}
		}
		if err := applyClientGroupTx(tx, records, group); err != nil {
			return err
		}
		affected = len(records)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return affected, nil
}

func shiftGroupBaseline(tx *gorm.DB, name string, up, down int64) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "name"}},
		DoUpdates: clause.Assignments(map[string]any{
			"reset_up":   gorm.Expr("client_groups.reset_up + ?", up),
			"reset_down": gorm.Expr("client_groups.reset_down + ?", down),
		}),
	}).Create(&model.ClientGroup{Name: name, ResetUp: up, ResetDown: down}).Error
}

// Keep normalized records and the legacy inbound projection in one transaction.
// A malformed projection must fail rather than leave two different group names.
func applyClientGroupTx(tx *gorm.DB, records []model.ClientRecord, group string) error {
	emails := make([]string, 0, len(records))
	emailSet := make(map[string]struct{}, len(records))
	for _, rec := range records {
		emails = append(emails, rec.Email)
		emailSet[rec.Email] = struct{}{}
	}
	ids := make(map[int]struct{})
	for _, batch := range chunkStrings(emails, sqlInChunk) {
		if err := tx.Model(&model.ClientRecord{}).Where("email IN ?", batch).
			Updates(map[string]any{"group_name": group, "updated_at": time.Now().UnixMilli()}).Error; err != nil {
			return err
		}
		var part []int
		if err := tx.Table("client_inbounds").
			Joins("JOIN clients ON clients.id = client_inbounds.client_id").
			Where("clients.email IN ?", batch).Distinct("client_inbounds.inbound_id").
			Pluck("inbound_id", &part).Error; err != nil {
			return err
		}
		for _, id := range part {
			ids[id] = struct{}{}
		}
	}
	ordered := make([]int, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Ints(ordered)
	for _, id := range ordered {
		var ib model.Inbound
		if err := tx.First(&ib, id).Error; err != nil {
			return err
		}
		var settings map[string]any
		if err := json.Unmarshal([]byte(ib.Settings), &settings); err != nil {
			return fmt.Errorf("inbound %d settings: %w", id, err)
		}
		clients, _ := settings["clients"].([]any)
		modified := false
		for _, item := range clients {
			cm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			email, _ := cm["email"].(string)
			if _, hit := emailSet[email]; !hit {
				continue
			}
			if group == "" {
				delete(cm, "group")
			} else {
				cm["group"] = group
			}
			modified = true
		}
		if modified {
			raw, err := json.Marshal(settings)
			if err != nil {
				return err
			}
			if err := tx.Model(&model.Inbound{}).Where("id = ?", id).UpdateColumn("settings", string(raw)).Error; err != nil {
				return err
			}
		}
		if ib.NodeID != nil {
			if err := (&NodeService{}).MarkNodeDirtyTx(tx, *ib.NodeID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *ClientService) replaceGroupValue(oldName, newName string) (int, error) {
	var affected int
	err := runSerializedTx(func(tx *gorm.DB) error {
		exists, err := groupExistsTx(tx, oldName)
		if err != nil {
			return err
		}
		if !exists {
			return common.NewError("group not found")
		}
		if newName != "" {
			exists, err := groupExistsTx(tx, newName)
			if err != nil {
				return err
			}
			if exists {
				return common.NewError("group already exists")
			}
		}
		var records []model.ClientRecord
		if err := tx.Where("group_name = ?", oldName).Find(&records).Error; err != nil {
			return err
		}
		if err := applyClientGroupTx(tx, records, newName); err != nil {
			return err
		}
		if newName == "" {
			if err := tx.Where("name = ?", oldName).Delete(&model.ClientGroup{}).Error; err != nil {
				return err
			}
		} else {
			res := tx.Model(&model.ClientGroup{}).Where("name = ?", oldName).Update("name", newName)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				if err := tx.Create(&model.ClientGroup{Name: newName}).Error; err != nil {
					return err
				}
			}
		}
		affected = len(records)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return affected, nil
}
