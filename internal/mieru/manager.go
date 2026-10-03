package mieru

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

type PortBinding struct {
	Port      int
	PortRange string
	Protocol  string
}

type User struct {
	Name     string
	Password string
}

type Instance struct {
	Id                  int
	Tag                 string
	Listen              string
	PortBindings        []PortBinding
	Users               []User
	MTU                 int
	LoggingLevel        string
	UserHintIsMandatory bool
}

func stringSlice(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func portEntryString(v any) string {
	switch n := v.(type) {
	case float64:
		if n == float64(int(n)) {
			return strconv.Itoa(int(n))
		}
	case int:
		return strconv.Itoa(n)
	case string:
		return strings.TrimSpace(n)
	}
	return ""
}

func parsePortBinding(value string, protocol string) (PortBinding, bool) {
	value = strings.TrimSpace(value)
	protocol = strings.ToUpper(strings.TrimSpace(protocol))
	if protocol != "TCP" && protocol != "UDP" || value == "" {
		return PortBinding{}, false
	}
	if strings.Contains(value, "-") {
		parts := strings.SplitN(value, "-", 2)
		if len(parts) != 2 {
			return PortBinding{}, false
		}
		start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil || start < 1025 || end > 65535 || end < start {
			return PortBinding{}, false
		}
		return PortBinding{PortRange: fmt.Sprintf("%d-%d", start, end), Protocol: protocol}, true
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1025 || port > 65535 {
		return PortBinding{}, false
	}
	return PortBinding{Port: port, Protocol: protocol}, true
}

func addPortBindings(dst *[]PortBinding, seen map[string]struct{}, values []any, protocol string) {
	for _, raw := range values {
		value := portEntryString(raw)
		binding, ok := parsePortBinding(value, protocol)
		if !ok {
			continue
		}
		key := binding.Protocol + ":"
		if binding.PortRange != "" {
			key += binding.PortRange
		} else {
			key += strconv.Itoa(binding.Port)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		*dst = append(*dst, binding)
	}
}

func portBindingsFromSettings(raw map[string]any, primaryPort int) []PortBinding {
	var bindings []PortBinding
	seen := map[string]struct{}{}
	if values, ok := raw["tcpPorts"].([]any); ok {
		addPortBindings(&bindings, seen, values, "TCP")
	}
	if values, ok := raw["udpPorts"].([]any); ok {
		addPortBindings(&bindings, seen, values, "UDP")
	}

	// Backward compatibility for inbounds created before protocol-specific
	// port bindings were added.
	if len(bindings) == 0 {
		protocols := stringSlice(raw["protocols"])
		if len(protocols) == 0 {
			protocols = []string{"TCP", "UDP"}
		}
		var legacyPorts []string
		legacyPorts = append(legacyPorts, strconv.Itoa(primaryPort))
		if rawPorts, ok := raw["additionalPorts"].([]any); ok {
			for _, value := range rawPorts {
				if s := portEntryString(value); s != "" {
					legacyPorts = append(legacyPorts, s)
				}
			}
		}
		for _, port := range legacyPorts {
			for _, protocol := range protocols {
				if binding, ok := parsePortBinding(port, protocol); ok {
					key := binding.Protocol + ":" + strconv.Itoa(binding.Port)
					if _, exists := seen[key]; exists {
						continue
					}
					seen[key] = struct{}{}
					bindings = append(bindings, binding)
				}
			}
		}
	}
	return bindings
}

// PortBindingsFromInbound reports the actual TCP/UDP listeners even if the
// inbound has no active users yet. Port conflict checks must reserve them.
func PortBindingsFromInbound(ib *model.Inbound) []PortBinding {
	if ib == nil || ib.Protocol != model.Mieru {
		return nil
	}
	var raw map[string]any
	if json.Unmarshal([]byte(ib.Settings), &raw) != nil {
		return nil
	}
	return portBindingsFromSettings(raw, ib.Port)
}

// ValidatePortBindings rejects settings that would leave mita with no
// listener. Existing rows may be read leniently, but new saves must not
// silently create an enabled inbound that cannot start.
func ValidatePortBindings(ib *model.Inbound) error {
	if ib == nil || ib.Protocol != model.Mieru {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(ib.Settings), &raw); err != nil {
		return fmt.Errorf("invalid Mieru settings: %w", err)
	}
	for _, entry := range []struct {
		field, protocol string
	}{{"tcpPorts", "TCP"}, {"udpPorts", "UDP"}} {
		values, _ := raw[entry.field].([]any)
		for _, value := range values {
			if _, ok := parsePortBinding(portEntryString(value), entry.protocol); !ok {
				return fmt.Errorf("invalid Mieru %s port %q: use 1025-65535", entry.protocol, portEntryString(value))
			}
		}
	}
	if len(portBindingsFromSettings(raw, ib.Port)) == 0 {
		return fmt.Errorf("Mieru needs a TCP or UDP listener on a port from 1025 to 65535")
	}
	return nil
}

func InstanceFromInbound(ib *model.Inbound) (Instance, bool) {
	if ib == nil || ib.Protocol != model.Mieru {
		return Instance{}, false
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(ib.Settings), &raw); err != nil {
		return Instance{}, false
	}
	bindings := portBindingsFromSettings(raw, ib.Port)

	clients, _ := raw["clients"].([]any)
	users := make([]User, 0, len(clients))
	for _, item := range clients {
		client, _ := item.(map[string]any)
		enabled, _ := client["enable"].(bool)
		email, _ := client["email"].(string)
		password, _ := client["password"].(string)
		if enabled && strings.TrimSpace(email) != "" && password != "" {
			users = append(users, User{Name: strings.TrimSpace(email), Password: password})
		}
	}
	if len(bindings) == 0 || len(users) == 0 {
		return Instance{}, false
	}

	mtu, _ := raw["mtu"].(float64)
	if int(mtu) < 1280 || int(mtu) > 1400 {
		mtu = 1400
	}
	level, _ := raw["loggingLevel"].(string)
	level = strings.ToUpper(strings.TrimSpace(level))
	switch level {
	case "OFF":
		level = "FATAL"
	case "FATAL", "ERROR", "WARN", "INFO", "DEBUG", "TRACE":
	default:
		level = "INFO"
	}
	mandatory, _ := raw["userHintIsMandatory"].(bool)

	return Instance{
		Id:                  ib.Id,
		Tag:                 ib.Tag,
		Listen:              strings.TrimSpace(ib.Listen),
		PortBindings:        bindings,
		Users:               users,
		MTU:                 int(mtu),
		LoggingLevel:        level,
		UserHintIsMandatory: mandatory,
	}, true
}

func (inst Instance) restartFingerprint() string {
	parts := []string{
		inst.Listen,
		strconv.Itoa(inst.MTU),
		strconv.FormatBool(inst.UserHintIsMandatory),
	}
	for _, b := range inst.PortBindings {
		if b.PortRange != "" {
			parts = append(parts, b.Protocol+":"+b.PortRange)
		} else {
			parts = append(parts, fmt.Sprintf("%s:%d", b.Protocol, b.Port))
		}
	}
	slices.Sort(parts)
	return strings.Join(parts, "|")
}

func (inst Instance) fingerprint() string {
	parts := []string{inst.restartFingerprint(), inst.LoggingLevel}
	for _, u := range inst.Users {
		parts = append(parts, u.Name+"="+u.Password)
	}
	slices.Sort(parts)
	return strings.Join(parts, "|")
}

type (
	managed struct {
		proc      *Process
		tag       string
		fp        string
		restartFP string
	}
	Manager struct {
		mu      sync.Mutex
		procs   map[int]*managed
		lastErr map[int]string
		traffic map[int]map[string]trafficCursor
	}
)

var (
	once      sync.Once
	singleton *Manager
)

func GetManager() *Manager {
	once.Do(func() {
		singleton = &Manager{
			procs:   map[int]*managed{},
			lastErr: map[int]string{},
			traffic: map[int]map[string]trafficCursor{},
		}
	})
	return singleton
}
func configDir() string { return filepath.Join(config.GetBinFolderPath(), "mieru") }
func configPathForID(id int) string {
	return filepath.Join(configDir(), fmt.Sprintf("mita-%d.json", id))
}

func socketPathForID(id int) string {
	return filepath.Join(configDir(), fmt.Sprintf("mita-%d.sock", id))
}

func renderConfig(inst Instance) map[string]any {
	bindings := make([]any, 0, len(inst.PortBindings))
	for _, b := range inst.PortBindings {
		entry := map[string]any{"protocol": b.Protocol}
		if b.PortRange != "" {
			entry["portRange"] = b.PortRange
		} else {
			entry["port"] = b.Port
		}
		bindings = append(bindings, entry)
	}
	users := make([]any, 0, len(inst.Users))
	for _, u := range inst.Users {
		users = append(users, map[string]any{"name": u.Name, "password": u.Password})
	}
	out := map[string]any{
		"portBindings": bindings,
		"users":        users,
		"loggingLevel": inst.LoggingLevel,
		"mtu":          inst.MTU,
		"dns": map[string]any{
			"dualStack": "PREFER_IPv4",
		},
	}
	if inst.Listen != "" {
		out["listenIPAddress"] = inst.Listen
	}
	advanced := map[string]any{}
	if inst.UserHintIsMandatory {
		advanced["userHintIsMandatory"] = true
	}
	// Mieru's server config does not use these client-side tuning values;
	// they are exported to the shared link/client config below.
	if len(advanced) > 0 {
		out["advancedSettings"] = advanced
	}
	return out
}

func writeConfig(inst Instance) (string, error) {
	if err := os.MkdirAll(configDir(), 0o750); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(renderConfig(inst), "", "  ")
	if err != nil {
		return "", err
	}
	path := configPathForID(inst.Id)
	if err := os.WriteFile(path, append(data, '\n'), 0o640); err != nil {
		return "", err
	}
	return path, nil
}

func (m *Manager) ensureLocked(inst Instance) error {
	fp := inst.fingerprint()
	restartFP := inst.restartFingerprint()
	if cur := m.procs[inst.Id]; cur != nil && cur.proc != nil && cur.proc.IsRunning() {
		if cur.fp == fp {
			cur.tag = inst.Tag
			return nil
		}
		// Upstream mita allows users and loggingLevel to be reloaded without
		// disturbing active connections. Reuse that path when no restart-only
		// server setting changed (ports/MTU/listen/advanced settings).
		if cur.restartFP == restartFP {
			if _, err := writeConfig(inst); err == nil {
				if err := cur.proc.Reload(); err == nil {
					cur.tag = inst.Tag
					cur.fp = fp
					delete(m.lastErr, inst.Id)
					logger.Debugf("mieru: reloaded mita for inbound %d (%s)", inst.Id, inst.Tag)
					return nil
				} else {
					logger.Debug("mieru: mita reload failed, falling back to restart:", err)
				}
			}
		}
	}

	if cur := m.procs[inst.Id]; cur != nil {
		_ = cur.proc.Stop()
		delete(m.procs, inst.Id)
		delete(m.traffic, inst.Id)
	}
	path, err := writeConfig(inst)
	if err != nil {
		return err
	}
	proc := newProcess(path, inst.Tag, socketPathForID(inst.Id))
	if err := proc.Start(); err != nil {
		return err
	}
	m.procs[inst.Id] = &managed{proc: proc, tag: inst.Tag, fp: fp, restartFP: restartFP}
	delete(m.lastErr, inst.Id)
	logger.Infof("mieru: started mita for inbound %d (%s)", inst.Id, inst.Tag)
	return nil
}

func (m *Manager) Ensure(inst Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureLocked(inst)
}

func (m *Manager) Reconcile(desired []Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := map[int]Instance{}
	for _, inst := range desired {
		want[inst.Id] = inst
	}
	for id, cur := range m.procs {
		if _, ok := want[id]; !ok {
			_ = cur.proc.Stop()
			delete(m.procs, id)
			delete(m.traffic, id)
			_ = os.Remove(configPathForID(id))
			_ = os.Remove(socketPathForID(id))
		}
	}
	for _, inst := range desired {
		if err := m.ensureLocked(inst); err != nil && m.lastErr[inst.Id] != err.Error() {
			m.lastErr[inst.Id] = err.Error()
			logger.Warningf("mieru: failed to start inbound %d (%s): %v", inst.Id, inst.Tag, err)
		}
	}
}

func (m *Manager) Remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur := m.procs[id]; cur != nil {
		_ = cur.proc.Stop()
		delete(m.procs, id)
	}
	delete(m.traffic, id)
	_ = os.Remove(configPathForID(id))
	_ = os.Remove(socketPathForID(id))
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cur := range m.procs {
		_ = cur.proc.Stop()
		_ = os.Remove(configPathForID(id))
		_ = os.Remove(socketPathForID(id))
		delete(m.procs, id)
		delete(m.traffic, id)
	}
}

func (m *Manager) HasRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cur := range m.procs {
		if cur.proc != nil && cur.proc.IsRunning() {
			return true
		}
	}
	return false
}
