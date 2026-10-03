package service

import (
	"errors"
	"fmt"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func validateReorderIDs(ids []int) error {
	if len(ids) == 0 {
		return errors.New("ids must not be empty")
	}
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return fmt.Errorf("invalid id %d", id)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate id %d", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (s *InboundService) Reorder(userID int, ids []int) error {
	if userID <= 0 {
		return errors.New("invalid user id")
	}
	if err := validateReorderIDs(ids); err != nil {
		return err
	}

	tx := database.GetDB().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() { _ = tx.Rollback() }()

	var count int64
	if err := tx.Model(&model.Inbound{}).
		Where("user_id = ? AND id IN ?", userID, ids).
		Count(&count).Error; err != nil {
		return err
	}
	if int(count) != len(ids) {
		return errors.New("reorder set contains inbounds outside the current user")
	}

	// Set the incoming sequence to a compact 0..N-1 range. A transaction makes
	// the new order visible atomically even while traffic/websocket refreshes
	// are reading the list.
	for sortOrder, id := range ids {
		if err := tx.Model(&model.Inbound{}).
			Where("user_id = ? AND id = ?", userID, id).
			Update("sort_order", sortOrder).Error; err != nil {
			return err
		}
	}
	return tx.Commit().Error
}

func (s *NodeService) Reorder(ids []int) error {
	if err := validateReorderIDs(ids); err != nil {
		return err
	}

	tx := database.GetDB().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() { _ = tx.Rollback() }()

	var count int64
	if err := tx.Model(&model.Node{}).Where("id IN ?", ids).Count(&count).Error; err != nil {
		return err
	}
	if int(count) != len(ids) {
		return errors.New("reorder set contains unknown nodes")
	}

	for sortOrder, id := range ids {
		if err := tx.Model(&model.Node{}).
			Where("id = ?", id).
			Update("sort_order", sortOrder).Error; err != nil {
			return err
		}
	}
	return tx.Commit().Error
}
