package outbound

import (
	"encoding/json"
	"fmt"
	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
	"github.com/SawaMEN/3x-ui/v3/internal/util/json_util"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

func bridgeManagedProbeOutbounds(cfg *xray.Config) (func(), error) {
	var rows []map[string]any
	if err := json.Unmarshal(cfg.OutboundConfigs, &rows); err != nil {
		return nil, err
	}
	var releases []func()
	cleanup := func() {
		for _, release := range releases {
			release()
		}
	}
	for i, row := range rows {
		if !externalvpn.IsAdditionalOutbound(row) {
			continue
		}
		bridge, release, err := externalvpn.EnsureProbeOutbound(row)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("managed outbound %q probe: %w", row["tag"], err)
		}
		releases = append(releases, release)
		rows[i] = bridge
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		cleanup()
		return nil, err
	}
	cfg.OutboundConfigs = json_util.RawMessage(raw)
	return cleanup, nil
}
