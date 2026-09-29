package service

import (
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

// A traffic transaction can touch thousands of client rows. last_online is
// stamped before that transaction commits, so keep more than one poll interval
// of overlap to make a slow commit visible on the next sample. Re-reading rows
// is cheap and safe because the in-memory baseline turns repeats into zero
// deltas.
const clientTrafficLiveScanOverlapMillis int64 = 15_000

// clientTrafficLiveSampler derives one live delta stream from the durable
// client_traffics table. Every collector (Xray, sing-box and sidecars) already
// commits attributable client bytes there, so sampling the shared table avoids
// making the clients page depend on whichever core happened to produce the
// latest websocket frame.
var clientTrafficLiveSampler struct {
	sync.Mutex
	initialized bool
	lastScan    int64
	baseline    map[string]clientTrafficCounters
}

type clientTrafficCounters struct {
	up   int64
	down int64
}

// ResetClientTrafficLiveSampler drops the live-only baseline. Call this while
// no websocket client is connected so the first viewer establishes a fresh
// baseline instead of receiving an artificial speed spike accumulated while
// the page was closed.
func ResetClientTrafficLiveSampler() {
	clientTrafficLiveSampler.Lock()
	clientTrafficLiveSampler.initialized = false
	clientTrafficLiveSampler.lastScan = 0
	clientTrafficLiveSampler.baseline = nil
	clientTrafficLiveSampler.Unlock()
}

// SampleClientTrafficDeltas returns per-client byte deltas and the matching
// absolute rows since the previous sample. The database is the aggregation
// boundary: any current or future traffic collector that updates
// client_traffics automatically becomes visible on the clients page.
func (s *InboundService) SampleClientTrafficDeltas() ([]*xray.ClientTraffic, []*xray.ClientTraffic, error) {
	clientTrafficLiveSampler.Lock()
	defer clientTrafficLiveSampler.Unlock()

	if !clientTrafficLiveSampler.initialized {
		// Capture the boundary before reading. A write committed while the full
		// baseline is being loaded is then eligible for the first delta scan even
		// if its row was read just before that commit became visible.
		scanAt := time.Now().UnixMilli()
		rows, err := s.GetAllClientTraffics()
		if err != nil {
			return nil, nil, err
		}
		baseline := make(map[string]clientTrafficCounters, len(rows))
		for _, row := range rows {
			if row == nil || row.Email == "" {
				continue
			}
			baseline[row.Email] = clientTrafficCounters{up: row.Up, down: row.Down}
		}
		clientTrafficLiveSampler.baseline = baseline
		clientTrafficLiveSampler.lastScan = scanAt
		clientTrafficLiveSampler.initialized = true
		return []*xray.ClientTraffic{}, []*xray.ClientTraffic{}, nil
	}

	// Keep an overlap around the boundary. Rows seen repeatedly are harmless:
	// the baseline turns later observations into zero deltas, while the overlap
	// prevents a transaction that started before a scan but committed after it
	// from being skipped.
	cutoff := clientTrafficLiveSampler.lastScan - clientTrafficLiveScanOverlapMillis
	if cutoff < 0 {
		cutoff = 0
	}
	scanAt := time.Now().UnixMilli()

	var rows []*xray.ClientTraffic
	if err := database.GetDB().Model(&xray.ClientTraffic{}).
		Where("last_online >= ?", cutoff).
		Find(&rows).Error; err != nil {
		return nil, nil, err
	}

	deltas := make([]*xray.ClientTraffic, 0, len(rows))
	changed := make([]*xray.ClientTraffic, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.Email == "" {
			continue
		}
		previous, exists := clientTrafficLiveSampler.baseline[row.Email]
		clientTrafficLiveSampler.baseline[row.Email] = clientTrafficCounters{up: row.Up, down: row.Down}

		var up, down int64
		if !exists {
			// A row created after the initial snapshot contains only traffic that
			// happened after that snapshot, so its current counters are the delta.
			up, down = row.Up, row.Down
		} else {
			if row.Up >= previous.up {
				up = row.Up - previous.up
			} else {
				// Counter reset/renewal happened between samples. The current value
				// is traffic accumulated after the reset.
				up = row.Up
			}
			if row.Down >= previous.down {
				down = row.Down - previous.down
			} else {
				down = row.Down
			}
		}
		if up <= 0 && down <= 0 {
			continue
		}
		deltas = append(deltas, &xray.ClientTraffic{Email: row.Email, Up: up, Down: down})
		changed = append(changed, row)
	}
	clientTrafficLiveSampler.lastScan = scanAt
	return deltas, changed, nil
}
