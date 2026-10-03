package service

import (
	"errors"

	"github.com/SawaMEN/3x-ui/v3/internal/xray"
	"gorm.io/gorm"
)

func canonicalizeClientTraffic(tx *gorm.DB, traffics []*xray.ClientTraffic) ([]*xray.ClientTraffic, error) {
	byEmail := make(map[string]*xray.ClientTraffic, len(traffics))
	for _, traffic := range traffics {
		if traffic == nil {
			continue
		}
		email := traffic.Email
		if traffic.TuicTrafficID > 0 {
			var owner xray.ClientTraffic
			err := tx.Select("email").Where("id = ?", traffic.TuicTrafficID).Take(&owner).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			email = owner.Email
		}
		current := byEmail[email]
		if current == nil {
			copy := *traffic
			copy.Email = email
			copy.TuicUUID = ""
			copy.TuicInboundId = 0
			byEmail[email] = &copy
			continue
		}
		current.Up = sumTrafficDelta(current.Up, traffic.Up)
		current.Down = sumTrafficDelta(current.Down, traffic.Down)
		current.Enable = current.Enable || traffic.Enable
	}
	result := make([]*xray.ClientTraffic, 0, len(byEmail))
	for _, traffic := range byEmail {
		result = append(result, traffic)
	}
	return result, nil
}
