package tuic

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

type managed struct {
	proc         *Process
	relay        *udpRelay
	tag          string
	configPath   string
	structuralFP string
	usersFP      string
}

type Manager struct {
	mu           sync.Mutex
	procs        map[int]*managed
	lastStartErr map[int]string
	// pending holds the final counters drained from sidecars that were stopped
	// by Reconcile/StopAll. The next traffic snapshot consumes them together
	// with counters from still-running instances.
	pending TrafficSnapshot
}

var (
	managerInstance *Manager
	managerOnce     sync.Once
)

func GetManager() *Manager {
	managerOnce.Do(func() {
		managerInstance = &Manager{
			procs:        make(map[int]*managed),
			lastStartErr: make(map[int]string),
		}
		if n := killStrayTuicProcesses(GetBinaryPath()); n > 0 {
			logger.Warningf("tuic: terminated %d orphaned tuic-server process(es) from a previous run", n)
		}
	})
	return managerInstance
}

func (m *Manager) HasRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mg := range m.procs {
		if mg.proc != nil && mg.proc.IsRunning() {
			return true
		}
	}
	return false
}

func (m *Manager) Ensure(inst Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureLocked(inst)
}

func (m *Manager) ensureLocked(inst Instance) error {
	if len(inst.Clients) == 0 {
		m.removeLocked(inst.Id)
		return nil
	}

	structuralFP := inst.StructuralFingerprint()
	usersFP := inst.UsersFingerprint()

	uuidToEmail := make(map[string]string, len(inst.Clients))
	for _, c := range inst.Clients {
		if c.UUID != "" && c.Email != "" {
			uuidToEmail[c.UUID] = c.Email
		}
	}

	if existing, ok := m.procs[inst.Id]; ok && existing != nil {
		if existing.proc != nil && existing.proc.IsRunning() &&
			existing.structuralFP == structuralFP && existing.usersFP == usersFP {
			existing.tag = inst.Tag
			existing.proc.UpdateClients(uuidToEmail)
			return nil
		}
		// Quiesce the old sidecar first, then drain its last relay counters before
		// closing the sockets. Use the new tag for a same-ID restart so a tag edit
		// cannot strand the final inbound delta under a stale database key.
		m.stopManagedAndCaptureLocked(existing, inst.Tag)
		delete(m.procs, inst.Id)
	}

	proc, relay, configPath, err := m.startLocked(inst, uuidToEmail)
	if err != nil {
		if m.lastStartErr[inst.Id] != err.Error() {
			m.lastStartErr[inst.Id] = err.Error()
			logger.Warningf("tuic: failed to start tuic-server for inbound %d (%s): %v", inst.Id, inst.Tag, err)
		}
		return err
	}
	delete(m.lastStartErr, inst.Id)

	m.procs[inst.Id] = &managed{
		proc:         proc,
		relay:        relay,
		tag:          inst.Tag,
		configPath:   configPath,
		structuralFP: structuralFP,
		usersFP:      usersFP,
	}
	return nil
}

func (m *Manager) startLocked(inst Instance, uuidToEmail map[string]string) (*Process, *udpRelay, string, error) {
	port, err := freeLoopbackUDPPort()
	if err != nil {
		return nil, nil, "", fmt.Errorf("tuic: pick sidecar port for %d: %w", inst.Id, err)
	}
	upstream := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	configBytes, err := GenerateConfig(inst, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, nil, "", fmt.Errorf("tuic: generate config for %d: %w", inst.Id, err)
	}
	configPath, err := WriteConfigFile(inst.Id, configBytes)
	if err != nil {
		return nil, nil, "", fmt.Errorf("tuic: write config for %d: %w", inst.Id, err)
	}
	relay, err := startUDPRelay(inst.BindTo(), upstream, relayFlowIdle)
	if err != nil {
		_ = RemoveConfigFile(inst.Id)
		return nil, nil, "", fmt.Errorf("tuic: listen on %s for %d: %w", inst.BindTo(), inst.Id, err)
	}
	proc := newProcess(configPath, inst.Tag, uuidToEmail, relay.bindPeer)
	if err := proc.Start(); err != nil {
		relay.Close()
		_ = RemoveConfigFile(inst.Id)
		return nil, nil, "", err
	}
	return proc, relay, configPath, nil
}

func (m *Manager) stopManagedAndCaptureLocked(mg *managed, tag string) {
	if mg == nil {
		return
	}
	if mg.proc != nil && mg.proc.IsRunning() {
		_ = mg.proc.Stop()
	}
	if mg.relay == nil {
		return
	}

	up, down := mg.relay.CollectTraffic()
	if up > 0 || down > 0 {
		m.pending.Inbounds = append(m.pending.Inbounds, InboundTrafficDelta{
			Tag:  tag,
			Up:   up,
			Down: down,
		})
	}
	for _, d := range mg.relay.CollectClientTraffic() {
		m.pending.Clients = append(m.pending.Clients, ClientTrafficDelta{
			Tag:   tag,
			Email: d.Email,
			Up:    d.Up,
			Down:  d.Down,
		})
	}
	mg.relay.Close()
}

func (m *Manager) GetActiveClients(window time.Duration) ([]string, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var emails []string
	var tags []string
	for _, mg := range m.procs {
		if mg.proc != nil && mg.proc.IsRunning() {
			active := mg.proc.GetActiveEmails(window)
			if len(active) > 0 {
				emails = append(emails, active...)
				tags = append(tags, mg.tag)
			}
		}
	}
	return emails, tags
}

type InboundTrafficDelta struct {
	Tag  string
	Up   int64
	Down int64
}

type ClientTrafficDelta struct {
	Tag   string
	Email string
	Up    int64
	Down  int64
}

type TrafficSnapshot struct {
	Inbounds []InboundTrafficDelta
	Clients  []ClientTrafficDelta
}

// CollectTrafficSnapshot atomically samples both the aggregate relay counters
// and the per-email counters attributed from authenticated tuic-server peers.
// The two counter sets have independent cursors: an unbound flow is retained on
// the per-client side until authentication identifies it, while the inbound
// aggregate can still be committed every poll. Final counters drained during a
// sidecar restart/removal are returned first from m.pending.
func (m *Manager) CollectTrafficSnapshot() TrafficSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.pending
	m.pending = TrafficSnapshot{}
	for _, mg := range m.procs {
		if mg.relay == nil || mg.proc == nil || !mg.proc.IsRunning() {
			continue
		}
		deltaUp, deltaDown := mg.relay.CollectTraffic()
		if deltaUp > 0 || deltaDown > 0 {
			out.Inbounds = append(out.Inbounds, InboundTrafficDelta{
				Tag:  mg.tag,
				Up:   deltaUp,
				Down: deltaDown,
			})
		}
		for _, d := range mg.relay.CollectClientTraffic() {
			out.Clients = append(out.Clients, ClientTrafficDelta{
				Tag:   mg.tag,
				Email: d.Email,
				Up:    d.Up,
				Down:  d.Down,
			})
		}
	}
	return out
}

// CollectTraffic is kept for package/API compatibility with callers that only
// need inbound totals. It intentionally leaves pending/current per-client
// cursors untouched.
func (m *Manager) CollectTraffic() []InboundTrafficDelta {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.pending.Inbounds
	m.pending.Inbounds = nil
	for _, mg := range m.procs {
		if mg.relay != nil && mg.proc != nil && mg.proc.IsRunning() {
			deltaUp, deltaDown := mg.relay.CollectTraffic()
			if deltaUp > 0 || deltaDown > 0 {
				out = append(out, InboundTrafficDelta{
					Tag:  mg.tag,
					Up:   deltaUp,
					Down: deltaDown,
				})
			}
		}
	}
	return out
}

func (m *Manager) Remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeLocked(id)
}

func (m *Manager) removeLocked(id int) {
	if existing, ok := m.procs[id]; ok && existing != nil {
		m.stopManagedAndCaptureLocked(existing, existing.tag)
		_ = RemoveConfigFile(id)
		delete(m.procs, id)
		delete(m.lastStartErr, id)
	}
}

func (m *Manager) Reconcile(desired []Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()

	desiredMap := make(map[int]Instance, len(desired))
	for _, inst := range desired {
		desiredMap[inst.Id] = inst
	}

	for id := range m.procs {
		if _, ok := desiredMap[id]; !ok {
			m.removeLocked(id)
		}
	}

	for _, inst := range desired {
		_ = m.ensureLocked(inst)
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, mg := range m.procs {
		m.stopManagedAndCaptureLocked(mg, mg.tag)
		_ = RemoveConfigFile(id)
	}
	m.procs = make(map[int]*managed)
}
