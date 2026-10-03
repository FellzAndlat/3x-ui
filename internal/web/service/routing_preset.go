package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

const (
	routingPresetMaxBytes = 128 << 10
	routingPresetMaxCount = 100
)

type RoutingPresetInput struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Rules       []json.RawMessage `json:"rules"`
}

type RoutingPresetView struct {
	Id          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RuleCount   int    `json:"ruleCount"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type RoutingPresetDetail struct {
	RoutingPresetView
	Rules []json.RawMessage `json:"rules"`
}

type RoutingPresetService struct{}

func validateRoutingRules(rules []json.RawMessage) ([]json.RawMessage, error) {
	if len(rules) > 2000 {
		return nil, errors.New("routing preset may contain at most 2000 rules")
	}
	out := make([]json.RawMessage, 0, len(rules))
	total := 2
	for i, raw := range rules {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 {
			return nil, fmt.Errorf("rule %d is empty", i+1)
		}
		var obj map[string]any
		if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
			return nil, fmt.Errorf("rule %d must be a JSON object", i+1)
		}
		clean, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("rule %d could not be normalized: %w", i+1, err)
		}
		total += len(clean) + 1
		if total > routingPresetMaxBytes {
			return nil, fmt.Errorf("routing preset is larger than %d KiB", routingPresetMaxBytes>>10)
		}
		out = append(out, json.RawMessage(clean))
	}
	return out, nil
}

func validateRoutingPresetInput(in *RoutingPresetInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 120 {
		return errors.New("name must be between 1 and 120 characters")
	}
	in.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(in.Description) > 1000 {
		return errors.New("description must not exceed 1000 characters")
	}
	return nil
}

func routingPresetToView(row *model.RoutingPreset) RoutingPresetView {
	var rules []json.RawMessage
	_ = json.Unmarshal([]byte(row.Rules), &rules)
	return RoutingPresetView{
		Id: row.Id, Name: row.Name, Description: row.Description,
		RuleCount: len(rules), CreatedAt: row.CreatedAt.Unix(), UpdatedAt: row.UpdatedAt.Unix(),
	}
}

func (s *RoutingPresetService) List(userID int) ([]RoutingPresetView, error) {
	var rows []model.RoutingPreset
	if err := database.GetDB().Where("user_id = ?", userID).Order("name ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]RoutingPresetView, 0, len(rows))
	for i := range rows {
		out = append(out, routingPresetToView(&rows[i]))
	}
	return out, nil
}

func (s *RoutingPresetService) Get(userID, id int) (*RoutingPresetDetail, error) {
	var row model.RoutingPreset
	if err := database.GetDB().Where("user_id = ? AND id = ?", userID, id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("routing preset not found")
		}
		return nil, err
	}
	var rules []json.RawMessage
	if err := json.Unmarshal([]byte(row.Rules), &rules); err != nil {
		return nil, err
	}
	return &RoutingPresetDetail{RoutingPresetView: routingPresetToView(&row), Rules: rules}, nil
}

func (s *RoutingPresetService) Save(userID int, in RoutingPresetInput) (*RoutingPresetView, error) {
	if err := validateRoutingPresetInput(&in); err != nil {
		return nil, err
	}
	rules, err := validateRoutingRules(in.Rules)
	if err != nil {
		return nil, err
	}

	db := database.GetDB()
	var count int64
	if err := db.Model(&model.RoutingPreset{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count >= routingPresetMaxCount {
		return nil, errors.New("routing preset limit reached")
	}

	encoded, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	row := &model.RoutingPreset{
		UserId: userID, Name: in.Name, Description: in.Description,
		Rules: string(encoded), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(row).Error; err != nil {
		return nil, err
	}
	result := routingPresetToView(row)
	return &result, nil
}

func (s *RoutingPresetService) Delete(userID, id int) error {
	res := database.GetDB().Where("user_id = ? AND id = ?", userID, id).Delete(&model.RoutingPreset{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("routing preset not found")
	}
	return nil
}
