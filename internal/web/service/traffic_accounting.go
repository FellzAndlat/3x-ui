package service

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"

	"gorm.io/gorm"
)

// XrayTrafficRead keeps the Xray absolute-counter baseline locked until the
// matching database transaction is either committed or rolled back.
type XrayTrafficRead struct {
	service    *XrayService
	checkpoint map[string]int64
	once       sync.Once
}

func cloneTrafficBaseline(src map[string]int64) map[string]int64 {
	dst := make(map[string]int64, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

// BeginXrayTrafficRead fetches one traffic delta while retaining the traffic
// mutex. The caller must Commit after the database write succeeds or Rollback
// after it fails, preventing a failed write from consuming the Xray delta.
func (s *XrayService) BeginXrayTrafficRead() ([]*xray.Traffic, []*xray.ClientTraffic, *XrayTrafficRead, error) {
	process := currentXrayProcess()
	if process == nil || !process.IsRunning() {
		err := errors.New("xray is not running")
		logger.Debug("Attempted to fetch Xray traffic, but Xray is not running:", err)
		return nil, nil, nil, err
	}

	s.xrayTrafficMu.Lock()
	apiPort := process.GetAPIPort()
	if err := s.xrayAPI.Init(apiPort); err != nil {
		s.xrayTrafficMu.Unlock()
		logger.Debug("Failed to initialize Xray API:", err)
		return nil, nil, nil, err
	}

	checkpoint := cloneTrafficBaseline(s.xrayAPI.StatsLastValues)
	traffics, clientTraffics, err := s.xrayAPI.GetTraffic()
	if err != nil {
		s.xrayTrafficMu.Unlock()
		logger.Debug("Failed to fetch Xray traffic:", err)
		return nil, nil, nil, err
	}

	return traffics, clientTraffics, &XrayTrafficRead{
		service:    s,
		checkpoint: checkpoint,
	}, nil
}

func (r *XrayTrafficRead) Commit() {
	if r == nil || r.service == nil {
		return
	}
	r.once.Do(func() {
		r.service.xrayTrafficMu.Unlock()
	})
}

func (r *XrayTrafficRead) Rollback() {
	if r == nil || r.service == nil {
		return
	}
	r.once.Do(func() {
		r.service.xrayAPI.StatsLastValues = cloneTrafficBaseline(r.checkpoint)
		r.service.xrayTrafficMu.Unlock()
	})
}

// CommitXrayTraffic persists inbound, client and outbound traffic in one DB
// transaction, giving one durable acknowledgement point for the Xray delta.
func (s *InboundService) CommitXrayTraffic(inboundTraffics []*xray.Traffic, clientTraffics []*xray.ClientTraffic) error {
	return submitTrafficWrite(func() error {
		return database.GetDB().Transaction(func(tx *gorm.DB) error {
			if err := s.addInboundTraffic(tx, inboundTraffics); err != nil {
				return fmt.Errorf("persist inbound traffic: %w", err)
			}
			if err := s.addClientTrafficStrict(tx, clientTraffics); err != nil {
				return fmt.Errorf("persist client traffic: %w", err)
			}
			if err := addOutboundTrafficStrict(tx, inboundTraffics); err != nil {
				return fmt.Errorf("persist outbound traffic: %w", err)
			}
			return nil
		})
	})
}

// addClientTrafficStrict mirrors the accounting path but never swallows an
// UPDATE error. Otherwise a failed client update looks like a successful
// transaction to the Xray delta reader and those bytes are lost.
func (s *InboundService) addClientTrafficStrict(tx *gorm.DB, traffics []*xray.ClientTraffic) error {
	if len(traffics) == 0 {
		return nil
	}

	emails := make([]string, 0, len(traffics))
	for _, traffic := range traffics {
		if traffic == nil || traffic.Email == "" {
			continue
		}
		emails = append(emails, traffic.Email)
	}
	if len(emails) == 0 {
		return nil
	}

	dbClientTraffics := make([]*xray.ClientTraffic, 0, len(emails))
	if err := tx.Model(xray.ClientTraffic{}).
		Where("email IN (?)", emails).
		Find(&dbClientTraffics).Error; err != nil {
		return err
	}
	if len(dbClientTraffics) == 0 {
		return nil
	}

	var err error
	var convertedExpiryByEmail map[string]int64
	dbClientTraffics, convertedExpiryByEmail, err = s.adjustTraffics(tx, dbClientTraffics)
	if err != nil {
		return err
	}

	trafficByEmail := make(map[string]*xray.ClientTraffic, len(traffics))
	for _, traffic := range traffics {
		if traffic == nil || traffic.Email == "" {
			continue
		}
		if current, ok := trafficByEmail[traffic.Email]; ok {
			current.Up = saturatingTrafficDelta(current.Up, traffic.Up)
			current.Down = saturatingTrafficDelta(current.Down, traffic.Down)
			continue
		}
		copyTraffic := *traffic
		trafficByEmail[traffic.Email] = &copyTraffic
	}

	now := time.Now().UnixMilli()
	for _, ct := range dbClientTraffics {
		if ct == nil {
			continue
		}
		traffic, ok := trafficByEmail[ct.Email]
		if !ok || (traffic.Up == 0 && traffic.Down == 0) {
			continue
		}

		// The SQL clamp cannot protect PostgreSQL from overflowing while it
		// evaluates "stored + delta". Limit each delta against the durable row
		// first, just like the non-strict AddTraffic path does. Otherwise a
		// near-saturated client makes the transaction fail forever and the Xray
		// baseline keeps rolling back to the same unacknowledged delta.
		up := clampTrafficDeltaToStored(ct.Up, traffic.Up)
		down := clampTrafficDeltaToStored(ct.Down, traffic.Down)
		if up == 0 && down == 0 {
			continue
		}
		if err := tx.Exec(
			fmt.Sprintf(
				`UPDATE client_traffics SET up = %s, down = %s, last_online = %s WHERE email = ?`,
				database.ClampedAddExpr("up"),
				database.ClampedAddExpr("down"),
				database.GreatestExpr("last_online", "?"),
			),
			up, down, now, ct.Email,
		).Error; err != nil {
			return err
		}
	}

	for _, email := range slices.Sorted(maps.Keys(convertedExpiryByEmail)) {
		if err := tx.Exec(
			`UPDATE client_traffics SET expiry_time = ? WHERE email = ? AND expiry_time < 0`,
			convertedExpiryByEmail[email], email,
		).Error; err != nil {
			return err
		}
	}
	return nil
}

func saturatingTrafficDelta(a, b int64) int64 {
	if b <= 0 {
		return a
	}
	if a >= database.TrafficMax || b > database.TrafficMax-a {
		return database.TrafficMax
	}
	return a + b
}

// clampTrafficDeltaToStored returns the part of delta that can still be added
// to an already stored counter without making the intermediate SQL addition
// exceed TrafficMax. Invalid/negative stored values are treated as zero usage;
// negative deltas are never applied.
func clampTrafficDeltaToStored(stored, delta int64) int64 {
	if delta <= 0 || stored >= database.TrafficMax {
		return 0
	}
	if stored < 0 {
		stored = 0
	}
	remaining := database.TrafficMax - stored
	if delta > remaining {
		return remaining
	}
	return delta
}

func addOutboundTrafficStrict(tx *gorm.DB, traffics []*xray.Traffic) error {
	for _, traffic := range traffics {
		if traffic == nil || !traffic.IsOutbound || traffic.Tag == "" {
			continue
		}

		var stored model.OutboundTraffics
		if err := tx.Model(&model.OutboundTraffics{}).
			Where("tag = ?", traffic.Tag).
			FirstOrCreate(&stored, model.OutboundTraffics{Tag: traffic.Tag}).Error; err != nil {
			return err
		}

		stored.Tag = traffic.Tag
		stored.Up = saturatingTrafficDelta(stored.Up, traffic.Up)
		stored.Down = saturatingTrafficDelta(stored.Down, traffic.Down)
		stored.Total = saturatingTrafficDelta(stored.Up, stored.Down)
		if err := tx.Save(&stored).Error; err != nil {
			return err
		}
	}
	return nil
}
