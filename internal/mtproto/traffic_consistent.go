package mtproto

import (
	"maps"
	"sync"
)

var consistentTrafficCollectMu sync.Mutex

// mergeCounterSnapshots advances one successful Telemt /v1/users snapshot
// while keeping baselines for users omitted from that particular response.
// Telemt exposes a cumulative total_octets counter per user.
func mergeCounterSnapshots(previous map[string]clientCounters, users map[string]statsUser) (map[string]clientCounters, map[string]clientCounters, []string) {
	next := make(map[string]clientCounters, len(previous)+len(users))
	maps.Copy(next, previous)
	deltas := make(map[string]clientCounters, len(users))
	online := make([]string, 0, len(users))

	for username, user := range users {
		current := clientCounters{total: int64(user.TotalOctets)}
		next[username] = current
		if user.Connections > 0 {
			online = append(online, username)
		}

		prev, had := previous[username]
		if !had {
			continue
		}
		delta := clientCounters{total: monotonicCounterDelta(current.total, prev.total)}
		if delta.total > 0 {
			deltas[username] = delta
		}
	}
	return next, deltas, online
}

// CollectTrafficConsistent is the race-safe traffic collector used by the web
// job. It preserves cumulative baselines across temporarily incomplete Telemt
// API responses and discards a scrape if the owning process was replaced while
// the HTTP request was in flight.
func (m *Manager) CollectTrafficConsistent() ([]Traffic, []string) {
	consistentTrafficCollectMu.Lock()
	defer consistentTrafficCollectMu.Unlock()

	type snap struct {
		id       int
		apiPort  int
		apiToken string
		owner    *managed
		last     map[string]clientCounters
		users    map[string]string
	}

	m.mu.Lock()
	snaps := make([]snap, 0, len(m.procs))
	for id, cur := range m.procs {
		if cur.proc == nil || !cur.proc.IsRunning() {
			continue
		}
		lastCopy := make(map[string]clientCounters, len(cur.last))
		maps.Copy(lastCopy, cur.last)
		usersCopy := make(map[string]string, len(cur.users))
		maps.Copy(usersCopy, cur.users)
		snaps = append(snaps, snap{
			id:       id,
			apiPort:  cur.apiPort,
			apiToken: cur.apiToken,
			owner:    cur,
			last:     lastCopy,
			users:    usersCopy,
		})
	}
	m.mu.Unlock()

	var out []Traffic
	var online []string
	for _, s := range snaps {
		users, ok := scrapeStats(s.apiPort, s.apiToken)
		if !ok {
			continue
		}
		next, deltas, instanceOnline := mergeCounterSnapshots(s.last, users)

		// Reconcile may replace the process while scrapeStats is in flight. Never
		// seed a newly started process with counters from the old process.
		m.mu.Lock()
		cur, stillSameProcess := m.procs[s.id]
		if !stillSameProcess || cur != s.owner {
			m.mu.Unlock()
			continue
		}
		cur.last = next
		tag := cur.tag
		m.mu.Unlock()

		for _, username := range instanceOnline {
			if email, exists := s.users[username]; exists {
				online = append(online, email)
			}
		}
		for username, delta := range deltas {
			email, exists := s.users[username]
			if !exists {
				continue
			}
			out = append(out, Traffic{Tag: tag, Email: email, Down: delta.total})
		}
	}
	return out, online
}
