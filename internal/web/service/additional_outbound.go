package service

import (
	"encoding/json"
	"fmt"

	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func transformAdditionalOutbounds(cfg *xray.Config) error {
	var rows []map[string]any
	if err := json.Unmarshal(cfg.OutboundConfigs, &rows); err != nil {
		return err
	}
	desired := map[string]bool{}
	for i, row := range rows {
		if !externalvpn.IsAdditionalOutbound(row) {
			continue
		}
		tag, _ := row["tag"].(string)
		desired[tag] = true
		bridge, err := externalvpn.EnsureOutbound(row)
		if err != nil {
			return fmt.Errorf("outbound %q: %w", tag, err)
		}
		rows[i] = bridge
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	cfg.OutboundConfigs = json_util.RawMessage(raw)
	externalvpn.KeepOutbounds(desired)
	return nil
}
