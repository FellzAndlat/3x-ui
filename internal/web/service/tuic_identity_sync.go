package service

import (
	"fmt"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *ClientService) validateTuicIdentities(tx *gorm.DB, inboundID int, changed []model.Client, detached []string, prune bool) error {
	var inbound model.Inbound
	if err := tx.Select("protocol").Where("id = ?", inboundID).Take(&inbound).Error; err != nil {
		return err
	}
	if inbound.Protocol != model.TUIC {
		return nil
	}
	candidates := append([]model.Client(nil), changed...)
	if !prune {
		current, err := s.ListForInbound(tx, inboundID)
		if err != nil {
			return err
		}
		excluded := make(map[string]bool)
		for _, client := range changed {
			excluded[client.Email] = true
		}
		for _, email := range detached {
			excluded[email] = true
		}
		for _, client := range current {
			if !excluded[client.Email] {
				candidates = append(candidates, client)
			}
		}
	}
	seen := make(map[uuid.UUID]string)
	for _, client := range candidates {
		if client.ID == "" {
			continue
		}
		id, err := uuid.Parse(client.ID)
		if err != nil {
			return fmt.Errorf("TUIC: invalid client UUID")
		}
		if email, exists := seen[id]; exists && email != client.Email {
			return fmt.Errorf("TUIC: duplicate client UUID within inbound")
		}
		seen[id] = client.Email
	}
	return nil
}
