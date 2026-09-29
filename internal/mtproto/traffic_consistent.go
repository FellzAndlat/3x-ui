package mtproto

import (
	"maps"
	"sync"
)

var consistentTrafficCollectMu sync.Mutex

// mergeCounterSnapshots advances one successful /stats snapshot while keeping
// baselines for users omitted from that particular response. mtg counters are
// cumulative, so dropping an omitted user's baseline would lose all bytes
// accumulated between the last visible sample and the next one.
func mergeCounterSnapshots(previous map[string]clientCounters, users map[string]statsUser) (map[string]clientCounters, map[string]clientCounters, []string) {
	next := make(map[string]clientCounters, len(previous)+len(users))
	maps.Copy(next, previous)
	deltas := make(map[string]clientCounters, len(users))
	online := make([]string, 0, len(users))

	for email, user := range users {
		current := clientCounters{up: user.BytesIn, down: user.BytesOut}
		next[email] = current
		if user.Connections > 0 {
			online = append(online, email)
		}

		prev, had := previous[email]
		if !had {
			continue
		}
		delta := clientCounters{
			up:   monotonicCounterDelta(current.up, prev.up),
			down: monotonicCounterDelta(current.down, prev.down),
		}
		if delta.up > 0 || delta.down > 0 {
			deltas[email] = delta
		}
	}
	return next, deltas, online
}

// CollectTrafficConsistent is the race-safe traffic collector used by the web
// job. It preserves cumulative baselines across temporarily incomplete /stats
// responses and discards a scrape if the owning mtg process was replaced while
// the HTTP request was in flight.
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
