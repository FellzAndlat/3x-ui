package mtproto

import (
	"maps"
	"sync"
)

var consistentTrafficCollectMu sync.Mutex

// mergeCounterSnapshots advances one successful Telemt /v1/stats/users
// snapshot while keeping baselines for users omitted from that particular
// response. Telemt exposes total_octets as one cumulative bidirectional counter,
// so 3x-ui records the aggregate delta in Down and keeps Up at zero. This keeps
// quota/inbound totals exact without inventing a direction Telemt does not
// report.
func mergeCounterSnapshots(previous map[string]clientCounters, users map[string]statsUser) (map[string]clientCounters, map[string]clientCounters, []string) {
	next := make(map[string]clientCounters, len(previous)+len(users))
	maps.Copy(next, previous)
	deltas := make(map[string]clientCounters, len(users))
	online := make([]string, 0, len(users))

	for email, user := range users {
		current := clientCounters{down: user.TotalOctets}
		next[email] = current
		if user.CurrentConnections > 0 {
			online = append(online, email)
		}

		prev, had := previous[email]
		if !had {
			continue
		}
		delta := clientCounters{
			down: monotonicCounterDelta(current.down, prev.down),
		}
		if delta.down > 0 {
			deltas[email] = delta
		}
	}
	return next, deltas, online
}

// CollectTrafficConsistent is the race-safe traffic collector used by the web
// job. It preserves cumulative baselines across temporarily incomplete Telemt
// stats responses and discards a scrape if the owning Telemt process was
// replaced while the HTTP request was in flight.
func (m *Manager) CollectTrafficConsistent() ([]Traffic, []string) {
	// The scheduler already serializes its own calls, but keeping this guard in
	// the manager makes the baseline contract safe for any other caller too.
	consistentTrafficCollectMu.Lock()
	defer consistentTrafficCollectMu.Unlock()

	type snap struct {
		id       int
		apiPort  int
		apiToken string
		owner    *managed
		last     map[string]clientCounters
	}

	m.mu.Lock()
	snaps := make([]snap, 0, len(m.procs))
	for id, cur := range m.procs {
		if cur.proc == nil || !cur.proc.IsRunning() {
			continue
		}
		lastCopy := make(map[string]clientCounters, len(cur.last))
		maps.Copy(lastCopy, cur.last)
		snaps = append(snaps, snap{
			id:       id,
			apiPort:  cur.apiPort,
			apiToken: cur.apiToken,
			owner:    cur,
			last:     lastCopy,
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

		online = append(online, instanceOnline...)
		for email, delta := range deltas {
			out = append(out, Traffic{
				Tag:   tag,
				Email: email,
				Up:    delta.up,
				Down:  delta.down,
			})
		}
	}
	return out, online
}
