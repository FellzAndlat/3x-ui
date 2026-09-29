package mieru

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultOnlineGrace = 120 * time.Second

type TrafficDelta struct {
	Tag   string
	Email string
	Up    int64
	Down  int64
}

type trafficCursor struct {
	up          int64
	down        int64
	initialized bool
}

type userTrafficStats struct {
	up         int64
	down       int64
	lastActive time.Time
}

// CollectTraffic reads per-user rolling 24-hour counters from every running
// mita instance. The counters are cumulative within their rolling window, so
// only high-water deltas are returned. A process restart or counter/window
// reset initializes a new cursor rather than creating a false traffic spike.
func (m *Manager) CollectTraffic(desired []Instance) ([]TrafficDelta, []string) {
	now := time.Now()
	onlineSet := make(map[string]struct{})
	deltas := make([]TrafficDelta, 0)

	type scrapeResult struct {
		inst  Instance
		stats map[string]userTrafficStats
		ok    bool
	}

	results := make(chan scrapeResult, len(desired))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, inst := range desired {
		if !m.isRunning(inst.Id) {
			continue
		}
		wg.Add(1)
		go func(inst Instance) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			stats, ok := scrapeUsers(socketPathForID(inst.Id), configPathForID(inst.Id))
			results <- scrapeResult{inst: inst, stats: stats, ok: ok}
		}(inst)
	}
	wg.Wait()
	close(results)

	for result := range results {
		if !result.ok {
			continue
		}
		m.mu.Lock()
		cursors := m.traffic[result.inst.Id]
		if cursors == nil {
			cursors = make(map[string]trafficCursor)
			m.traffic[result.inst.Id] = cursors
		}
		instanceDeltas, instanceOnline := collectInstanceTraffic(result.inst, cursors, result.stats, now)
		deltas = append(deltas, instanceDeltas...)
		for _, email := range instanceOnline {
			onlineSet[email] = struct{}{}
		}
		m.mu.Unlock()
	}

	online := make([]string, 0, len(onlineSet))
	for email := range onlineSet {
		online = append(online, email)
	}
	return deltas, online
}

// collectInstanceTraffic advances the cumulative per-user cursors for one
// Mieru instance. A successful scrape is not guaranteed to contain every
// configured user, so cursors are retained for configured users that are
// temporarily absent from the output. This prevents traffic accrued while a
// user is omitted from one or more scrapes from being silently discarded when
// that user reappears.
func collectInstanceTraffic(inst Instance, cursors map[string]trafficCursor, stats map[string]userTrafficStats, now time.Time) ([]TrafficDelta, []string) {
	configured := make(map[string]struct{}, len(inst.Users))
	for _, user := range inst.Users {
		email := strings.TrimSpace(user.Name)
		if email != "" {
			configured[email] = struct{}{}
		}
	}

	deltas := make([]TrafficDelta, 0, len(stats))
	online := make([]string, 0, len(stats))
	for user, st := range stats {
		email := strings.TrimSpace(user)
		if email == "" {
			continue
		}
		if _, ok := configured[email]; !ok {
			continue
		}

		cur := cursors[email]
		var delta TrafficDelta
		if cur.initialized {
			if st.up > cur.up {
				delta.Up = st.up - cur.up
			}
			if st.down > cur.down {
				delta.Down = st.down - cur.down
			}
		}
		cur.up = st.up
		cur.down = st.down
		cur.initialized = true
		cursors[email] = cur

		if delta.Up > 0 || delta.Down > 0 {
			delta.Tag = inst.Tag
			delta.Email = email
			deltas = append(deltas, delta)
		}
		if !st.lastActive.IsZero() && now.Sub(st.lastActive) <= defaultOnlineGrace {
			online = append(online, email)
		}
	}

	// Only configuration removal invalidates a cursor. Temporary absence from a
	// successful `get users` response must preserve the last cumulative sample.
	for email := range cursors {
		if _, ok := configured[email]; !ok {
			delete(cursors, email)
		}
	}
	return deltas, online
}

func (m *Manager) isRunning(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.procs[id]
	return cur != nil && cur.proc != nil && cur.proc.IsRunning()
}

func scrapeUsers(socketPath, configPath string) (map[string]userTrafficStats, bool) {
	bin := GetBinaryPath()
	if _, err := os.Stat(bin); err != nil {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	absSocket, err := filepath.Abs(socketPath)
	if err != nil {
		return nil, false
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return nil, false
	}

	cmd := exec.CommandContext(ctx, bin, "get", "users")
	cmd.Env = append(os.Environ(),
		"MITA_CONFIG_JSON_FILE="+absConfig,
		"MITA_UDS_PATH="+absSocket,
		"MITA_INSECURE_UDS=true",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	return parseUsersTable(string(out)), true
}

func parseUsersTable(output string) map[string]userTrafficStats {
	result := make(map[string]userTrafficStats)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || strings.EqualFold(fields[0], "User") {
			continue
		}

		stats := userTrafficStats{}
		if ts, err := time.Parse(time.RFC3339, fields[1]); err == nil {
			stats.lastActive = ts
		}

		if compactByteToken(fields[2]) && compactByteToken(fields[3]) {
			stats.down = parseByteToken(fields[2])
			stats.up = parseByteToken(fields[3])
		} else if len(fields) >= 6 {
			stats.down = parseBytePair(fields[2], fields[3])
			stats.up = parseBytePair(fields[4], fields[5])
		} else {
			continue
		}
		result[fields[0]] = stats
	}
	return result
}

func compactByteToken(value string) bool {
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			return true
		}
	}
	return false
}

func parseByteToken(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return 0
	}
	i := 0
	for i < len(value) {
		c := value[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' {
			i++
			continue
		}
		break
	}
	if i == 0 || i == len(value) {
		return 0
	}
	return parseBytePair(value[:i], value[i:])
}

func parseBytePair(number, unit string) int64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
	if err != nil || value < 0 {
		return 0
	}
	var multiplier float64
	switch strings.TrimSpace(unit) {
	case "B":
		multiplier = 1
	case "KiB":
		multiplier = 1 << 10
	case "MiB":
		multiplier = 1 << 20
	case "GiB":
		multiplier = 1 << 30
	case "TiB":
		multiplier = 1 << 40
	case "PiB":
		multiplier = 1 << 50
	default:
		return 0
	}
	return int64(value * multiplier)
}
