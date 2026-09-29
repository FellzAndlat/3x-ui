package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/mieru"
)

const (
	firewallAutoSyncKey     = "firewallAutoSync"
	firewallManagedRulesKey = "firewallManagedRules"
	firewallManualRulesKey  = "firewallManualRules"
)

var firewallMu sync.Mutex

type FirewallRule struct {
	Port      int    `json:"port,omitempty"`
	PortRange string `json:"portRange,omitempty"`
	Protocol  string `json:"protocol"`
	Source    string `json:"source"`
	Label     string `json:"label"`
	Owned     bool   `json:"owned"`
	Exists    bool   `json:"exists"`
}

type FirewallManualRule struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

type FirewallStatus struct {
	Supported   bool                 `json:"supported"`
	Backend     string               `json:"backend"`
	Enabled     bool                 `json:"enabled"`
	AutoSync    bool                 `json:"autoSync"`
	Rules       []FirewallRule       `json:"rules"`
	ManualRules []FirewallManualRule `json:"manualRules"`
	Message     string               `json:"message,omitempty"`
}

type FirewallService struct{}

type firewallBackend struct {
	name, binary, offline, zone string
	enabled                     func(context.Context) (bool, error)
}

func (s *FirewallService) GetStatus(ctx context.Context, safetyPort int) (FirewallStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	return s.status(ctx, safetyPort)
}

func (s *FirewallService) SetEnabled(ctx context.Context, enabled bool, safetyPort int) (FirewallStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	b, err := detectFirewallBackend(ctx)
	if err != nil {
		return FirewallStatus{}, err
	}
	if enabled {
		if err := s.sync(ctx, b, safetyPort); err != nil {
			return FirewallStatus{}, err
		}
		if err := setFirewallBackendEnabled(ctx, b, true); err != nil {
			return FirewallStatus{}, err
		}
		if err := s.sync(ctx, b, safetyPort); err != nil {
			return FirewallStatus{}, err
		}
	} else if err := setFirewallBackendEnabled(ctx, b, false); err != nil {
		return FirewallStatus{}, err
	}
	return s.status(ctx, safetyPort)
}

func (s *FirewallService) SetAutoSync(ctx context.Context, enabled bool, safetyPort int) (FirewallStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	if err := (&SettingService{}).setBool(firewallAutoSyncKey, enabled); err != nil {
		return FirewallStatus{}, err
	}
	b, err := detectFirewallBackend(ctx)
	if err != nil {
		return FirewallStatus{}, err
	}
	if err := s.sync(ctx, b, safetyPort); err != nil {
		return FirewallStatus{}, err
	}
	return s.status(ctx, safetyPort)
}

func (s *FirewallService) Sync(ctx context.Context, safetyPort int) (FirewallStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	b, err := detectFirewallBackend(ctx)
	if err != nil {
		return FirewallStatus{}, err
	}
	if err := s.sync(ctx, b, safetyPort); err != nil {
		return FirewallStatus{}, err
	}
	return s.status(ctx, safetyPort)
}

func (s *FirewallService) SyncIfEnabled(ctx context.Context) error {
	auto, err := firewallAutoSync()
	if err != nil || !auto {
		return err
	}
	b, err := detectFirewallBackend(ctx)
	if err != nil {
		return nil
	}
	on, err := b.enabled(ctx)
	if err != nil || !on {
		return err
	}
	_, err = s.Sync(ctx, 0)
	return err
}

func (s *FirewallService) AddManualRule(ctx context.Context, port int, protocol string, safetyPort int) (FirewallStatus, error) {
	return s.changeManualRule(ctx, port, protocol, safetyPort, true)
}

func (s *FirewallService) DeleteManualRule(ctx context.Context, port int, protocol string, safetyPort int) (FirewallStatus, error) {
	return s.changeManualRule(ctx, port, protocol, safetyPort, false)
}

func (s *FirewallService) changeManualRule(ctx context.Context, port int, protocol string, safetyPort int, add bool) (FirewallStatus, error) {
	firewallMu.Lock()
	defer firewallMu.Unlock()
	if port < 1 || port > 65535 {
		return FirewallStatus{}, fmt.Errorf("invalid port %d", port)
	}
	protos, err := normalizeFirewallProtocols(protocol)
	if err != nil {
		return FirewallStatus{}, err
	}
	manual, err := loadManualFirewallRules()
	if err != nil {
		return FirewallStatus{}, err
	}
	set := make(map[string]FirewallManualRule, len(manual)+2)
	for _, r := range manual {
		set[firewallRuleKey(r.Port, r.Protocol)] = r
	}
	for _, proto := range protos {
		key := firewallRuleKey(port, proto)
		if add {
			set[key] = FirewallManualRule{Port: port, Protocol: proto}
		} else {
			delete(set, key)
		}
	}
	manual = manual[:0]
	for _, r := range set {
		manual = append(manual, r)
	}
	sort.Slice(manual, func(i, j int) bool {
		return manual[i].Port < manual[j].Port || (manual[i].Port == manual[j].Port && manual[i].Protocol < manual[j].Protocol)
	})
	if err := saveFirewallJSON(firewallManualRulesKey, manual); err != nil {
		return FirewallStatus{}, err
	}
	b, err := detectFirewallBackend(ctx)
	if err != nil {
		return FirewallStatus{}, err
	}
	if err := s.sync(ctx, b, safetyPort); err != nil {
		return FirewallStatus{}, err
	}
	return s.status(ctx, safetyPort)
}

func (s *FirewallService) status(ctx context.Context, safetyPort int) (FirewallStatus, error) {
	auto, err := firewallAutoSync()
	if err != nil {
		return FirewallStatus{}, err
	}
	manual, err := loadManualFirewallRules()
	if err != nil {
		return FirewallStatus{}, err
	}
	st := FirewallStatus{AutoSync: auto, ManualRules: manual}
	b, err := detectFirewallBackend(ctx)
	if err != nil {
		st.Message = err.Error()
		return st, nil
	}
	st.Supported, st.Backend = true, b.name
	st.Enabled, _ = b.enabled(ctx)
	desired, err := s.desiredRules(auto, safetyPort)
	if err != nil {
		return FirewallStatus{}, err
	}
	existing, err := listFirewallRules(ctx, b)
	if err != nil {
		return FirewallStatus{}, err
	}
	managed, err := loadManagedFirewallRules()
	if err != nil {
		return FirewallStatus{}, err
	}
	owned := map[string]bool{}
	for _, r := range managed {
		if key := firewallRuleSpec(r); key != "" {
			owned[key] = r.Owned
		}
	}
	seen := make(map[string]bool, len(desired))
	for i := range desired {
		key := firewallRuleSpec(desired[i])
		seen[key] = true
		desired[i].Exists, desired[i].Owned = existing[key], owned[key]
	}
	// When auto-sync is disabled, inbound rules are intentionally frozen rather
	// than deleted. Keep showing those managed rules in the UI so the operator
	// sees the actual firewall state instead of an incomplete desired-only view.
	if !auto {
		for _, r := range managed {
			key := firewallRuleSpec(r)
			if key == "" || r.Source != "inbound" || seen[key] {
				continue
			}
			r.Exists = existing[key]
			desired = append(desired, r)
			seen[key] = true
		}
		sortFirewallRules(desired)
	}
	st.Rules = desired
	return st, nil
}

func (s *FirewallService) sync(ctx context.Context, b firewallBackend, safetyPort int) error {
	auto, err := firewallAutoSync()
	if err != nil {
		return err
	}
	desired, err := s.desiredRules(auto, safetyPort)
	if err != nil {
		return err
	}
	existing, err := listFirewallRules(ctx, b)
	if err != nil {
		return err
	}
	managed, err := loadManagedFirewallRules()
	if err != nil {
		return err
	}
	old := map[string]FirewallRule{}
	for _, r := range managed {
		if key := firewallRuleSpec(r); key != "" {
			old[key] = r
		}
	}
	want := map[string]bool{}
	next := make([]FirewallRule, 0, len(desired)+len(managed))
	for _, r := range desired {
		key := firewallRuleSpec(r)
		if key == "" {
			continue
		}
		want[key] = true
		r.Owned = old[key].Owned
		if !existing[key] {
			if err := addFirewallRule(ctx, b, r); err != nil {
				return err
			}
			r.Owned = true
			existing[key] = true
		}
		r.Exists = true
		next = append(next, r)
	}
	for _, r := range managed {
		key := firewallRuleSpec(r)
		if key == "" || want[key] {
			continue
		}
		// Auto-sync off means freeze the last reconciled inbound rules. Manual
		// rule changes, firewall enable/disable and Sync-now must not silently
		// close working proxy ports merely because background automation is off.
		if !auto && r.Source == "inbound" {
			r.Exists = false
			next = append(next, r)
			continue
		}
		if !r.Owned || !existing[key] {
			continue
		}
		if err := deleteFirewallRule(ctx, b, r); err != nil {
			return err
		}
	}
	for i := range next {
		next[i].Exists = false
	}
	return saveFirewallJSON(firewallManagedRulesKey, next)
}

func (s *FirewallService) desiredRules(auto bool, safetyPort int) ([]FirewallRule, error) {
	m := map[string]FirewallRule{}
	addRule := func(r FirewallRule) {
		key := firewallRuleSpec(r)
		if key == "" {
			return
		}
		if old, ok := m[key]; ok && firewallSourcePriority(r.Source) <= firewallSourcePriority(old.Source) {
			return
		}
		m[key] = r
	}
	add := func(port int, proto, source, label string) {
		addRule(FirewallRule{Port: port, Protocol: proto, Source: source, Label: label})
	}
	settings := SettingService{}
	if p, err := settings.GetPort(); err == nil {
		add(p, "tcp", "panel", "Web panel")
	}
	if enabled, _ := settings.GetSubEnable(); enabled {
		if p, err := settings.GetSubPort(); err == nil {
			add(p, "tcp", "subscription", "Subscription service")
		}
	}
	if safetyPort > 0 {
		add(safetyPort, "tcp", "session", "Current panel connection")
	}
	for _, p := range detectSSHPorts() {
		add(p, "tcp", "ssh", "SSH")
	}
	manual, err := loadManualFirewallRules()
	if err != nil {
		return nil, err
	}
	for _, r := range manual {
		add(r.Port, r.Protocol, "manual", "Manual rule")
	}
	if auto {
		var inbounds []model.Inbound
		err := database.GetDB().Model(&model.Inbound{}).
			Select("port", "protocol", "stream_settings", "settings", "remark", "enable", "node_id").
			Where("enable = ? AND node_id IS NULL", true).Find(&inbounds).Error
		if err != nil {
			return nil, err
		}
		for _, ib := range inbounds {
			label := strings.TrimSpace(ib.Remark)
			if label == "" {
				label = string(ib.Protocol)
			}
			if ib.Protocol == model.Mieru {
				for _, binding := range mieru.PortBindingsFromInbound(&ib) {
					addRule(FirewallRule{
						Port:      binding.Port,
						PortRange: binding.PortRange,
						Protocol:  strings.ToLower(binding.Protocol),
						Source:    "inbound",
						Label:     label,
					})
				}
				continue
			}
			network, _ := inboundStreamHints(string(ib.Protocol), ib.StreamSettings, ib.Settings)
			for _, proto := range firewallProtocolsForInbound(ib.Protocol, network) {
				add(ib.Port, proto, "inbound", label)
			}
		}
	}
	out := make([]FirewallRule, 0, len(m))
	for _, r := range m {
		out = append(out, r)
	}
	sortFirewallRules(out)
	return out, nil
}

func firewallSourcePriority(s string) int {
	switch s {
	case "session":
		return 60
	case "panel":
		return 50
	case "ssh":
		return 40
	case "subscription":
		return 30
	case "manual":
		return 20
	case "inbound":
		return 10
	default:
		return 0
	}
}

func firewallProtocolsForInbound(protocol model.Protocol, network string) []string {
	switch protocol {
	case model.WireGuard, model.AmneziaWG, model.Hysteria, model.TUIC:
		return []string{"udp"}
	case model.Mixed, model.VKTurnProxy:
		return []string{"tcp", "udp"}
	}
	seen := map[string]bool{}
	for part := range strings.FieldsFuncSeq(strings.ToLower(network), func(r rune) bool { return r == ',' || r == ' ' || r == ';' || r == '/' }) {
		switch strings.TrimSpace(part) {
		case "udp", "kcp", "mkcp", "quic", "hysteria", "wireguard":
			seen["udp"] = true
		case "tcp", "ws", "websocket", "grpc", "http", "httpupgrade", "splithttp", "xhttp":
			seen["tcp"] = true
		}
	}
	if len(seen) == 0 {
		seen["tcp"] = true
	}
	out := []string{}
	if seen["tcp"] {
		out = append(out, "tcp")
	}
	if seen["udp"] {
		out = append(out, "udp")
	}
	return out
}

func normalizeFirewallProtocols(p string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "tcp":
		return []string{"tcp"}, nil
	case "udp":
		return []string{"udp"}, nil
	case "both", "tcp,udp", "udp,tcp":
		return []string{"tcp", "udp"}, nil
	default:
		return nil, errors.New("protocol must be tcp, udp, or both")
	}
}

func firewallRuleKey(port int, proto string) string {
	spec, ok := canonicalFirewallSpec(strconv.Itoa(port) + "/" + proto)
	if !ok {
		return ""
	}
	return spec
}

func firewallRuleSpec(r FirewallRule) string {
	if strings.TrimSpace(r.PortRange) != "" {
		spec, ok := canonicalFirewallSpec(r.PortRange + "/" + r.Protocol)
		if !ok {
			return ""
		}
		return spec
	}
	return firewallRuleKey(r.Port, r.Protocol)
}

func firewallRuleStart(r FirewallRule) int {
	if r.Port > 0 {
		return r.Port
	}
	raw := strings.TrimSpace(r.PortRange)
	if i := strings.IndexAny(raw, "-:"); i >= 0 {
		raw = raw[:i]
	}
	n, _ := strconv.Atoi(strings.TrimSpace(raw))
	return n
}

func sortFirewallRules(rules []FirewallRule) {
	sort.Slice(rules, func(i, j int) bool {
		pi, pj := firewallRuleStart(rules[i]), firewallRuleStart(rules[j])
		if pi != pj {
			return pi < pj
		}
		si, sj := firewallRuleSpec(rules[i]), firewallRuleSpec(rules[j])
		if si != sj {
			return si < sj
		}
		return rules[i].Source < rules[j].Source
	})
}

func canonicalFirewallSpec(spec string) (string, bool) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(spec)), "/")
	if len(parts) != 2 || (parts[1] != "tcp" && parts[1] != "udp") {
		return "", false
	}
	portSpec := strings.TrimSpace(parts[0])
	if portSpec == "" {
		return "", false
	}
	portSpec = strings.ReplaceAll(portSpec, ":", "-")
	if strings.Contains(portSpec, "-") {
		bounds := strings.Split(portSpec, "-")
		if len(bounds) != 2 {
			return "", false
		}
		start, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
		end, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
		if err1 != nil || err2 != nil || start < 1 || end > 65535 || end < start {
			return "", false
		}
		if start == end {
			return strconv.Itoa(start) + "/" + parts[1], true
		}
		return fmt.Sprintf("%d-%d/%s", start, end, parts[1]), true
	}
	port, err := strconv.Atoi(portSpec)
	if err != nil || port < 1 || port > 65535 {
		return "", false
	}
	return strconv.Itoa(port) + "/" + parts[1], true
}

func ufwFirewallSpec(spec string) string {
	parts := strings.SplitN(spec, "/", 2)
	if len(parts) != 2 {
		return spec
	}
	return strings.ReplaceAll(parts[0], "-", ":") + "/" + parts[1]
}

func firewallSetting(key, fallback string) (string, error) {
	st, err := (&SettingService{}).getSetting(key)
	if database.IsNotFound(err) {
		return fallback, nil
	}
	if err != nil {
		return "", err
	}
	return st.Value, nil
}

func firewallAutoSync() (bool, error) {
	raw, err := firewallSetting(firewallAutoSyncKey, "true")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(raw) == "" {
		return true, nil
	}
	return strconv.ParseBool(raw)
}

func loadManagedFirewallRules() ([]FirewallRule, error) {
	var out []FirewallRule
	raw, err := firewallSetting(firewallManagedRulesKey, "[]")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func loadManualFirewallRules() ([]FirewallManualRule, error) {
	var out []FirewallManualRule
	raw, err := firewallSetting(firewallManualRulesKey, "[]")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func saveFirewallJSON(key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return (&SettingService{}).setString(key, string(b))
}

func detectFirewallBackend(ctx context.Context) (firewallBackend, error) {
	var candidates []firewallBackend
	if path, err := exec.LookPath("ufw"); err == nil {
		b := firewallBackend{name: "ufw", binary: path}
		b.enabled = func(ctx context.Context) (bool, error) {
			out, err := runFirewallCommand(ctx, path, "status")
			return err == nil && strings.Contains(out, "Status: active"), err
		}
		candidates = append(candidates, b)
	}
	if path, err := exec.LookPath("firewall-cmd"); err == nil {
		b := firewallBackend{name: "firewalld", binary: path, zone: "public"}
		if p, err := exec.LookPath("firewall-offline-cmd"); err == nil {
			b.offline = p
		}
		b.enabled = func(ctx context.Context) (bool, error) {
			if systemctl, err := exec.LookPath("systemctl"); err == nil {
				cmd := exec.CommandContext(ctx, systemctl, "is-active", "--quiet", "firewalld")
				cmd.Env = append(os.Environ(), "LC_ALL=C")
				err := cmd.Run()
				if err == nil {
					return true, nil
				}
				if _, ok := err.(*exec.ExitError); ok {
					return false, nil
				}
				return false, err
			}
			out, err := runFirewallCommand(ctx, path, "--state")
			return err == nil && strings.TrimSpace(out) == "running", nil
		}
		if out, err := runFirewallCommand(ctx, path, "--get-default-zone"); err == nil && strings.TrimSpace(out) != "" {
			b.zone = strings.TrimSpace(out)
		} else if b.offline != "" {
			if out, err := runFirewallCommand(ctx, b.offline, "--get-default-zone"); err == nil && strings.TrimSpace(out) != "" {
				b.zone = strings.TrimSpace(out)
			}
		}
		candidates = append(candidates, b)
	}
	for _, b := range candidates {
		if on, _ := b.enabled(ctx); on {
			return b, nil
		}
	}
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	return firewallBackend{}, errors.New("no supported firewall found (install ufw or firewalld)")
}

func listFirewallRules(ctx context.Context, b firewallBackend) (map[string]bool, error) {
	out := map[string]bool{}
	if b.name == "ufw" {
		text, err := runFirewallCommand(ctx, b.binary, "show", "added")
		if err != nil {
			return nil, err
		}
		for line := range strings.SplitSeq(text, "\n") {
			f := strings.Fields(strings.TrimSpace(line))
			if len(f) >= 3 && f[0] == "ufw" && f[1] == "allow" {
				if spec, ok := canonicalFirewallSpec(f[2]); ok {
					out[spec] = true
				}
			}
		}
		return out, nil
	}
	on, _ := b.enabled(ctx)
	binary := b.binary
	if !on {
		if b.offline == "" {
			return out, nil
		}
		binary = b.offline
	}
	text, err := runFirewallCommand(ctx, binary, "--zone="+b.zone, "--list-ports")
	if err != nil {
		return nil, err
	}
	for _, raw := range strings.Fields(text) {
		if spec, ok := canonicalFirewallSpec(raw); ok {
			out[spec] = true
		}
	}
	return out, nil
}

func validFirewallSpec(spec string) bool {
	_, ok := canonicalFirewallSpec(spec)
	return ok
}

func addFirewallRule(ctx context.Context, b firewallBackend, r FirewallRule) error {
	spec := firewallRuleSpec(r)
	if spec == "" {
		return errors.New("invalid firewall rule")
	}
	if b.name == "ufw" {
		_, err := runFirewallCommand(ctx, b.binary, "allow", ufwFirewallSpec(spec), "comment", "3x-ui managed")
		return err
	}
	on, _ := b.enabled(ctx)
	if !on {
		if b.offline == "" {
			return errors.New("firewalld is stopped and firewall-offline-cmd is unavailable")
		}
		_, err := runFirewallCommand(ctx, b.offline, "--zone="+b.zone, "--add-port="+spec)
		return err
	}
	if _, err := runFirewallCommand(ctx, b.binary, "--permanent", "--zone="+b.zone, "--add-port="+spec); err != nil {
		return err
	}
	_, err := runFirewallCommand(ctx, b.binary, "--zone="+b.zone, "--add-port="+spec)
	return err
}

func deleteFirewallRule(ctx context.Context, b firewallBackend, r FirewallRule) error {
	spec := firewallRuleSpec(r)
	if spec == "" {
		return errors.New("invalid firewall rule")
	}
	if b.name == "ufw" {
		_, err := runFirewallCommand(ctx, b.binary, "--force", "delete", "allow", ufwFirewallSpec(spec))
		return err
	}
	on, _ := b.enabled(ctx)
	if !on {
		if b.offline == "" {
			return errors.New("firewalld is stopped and firewall-offline-cmd is unavailable")
		}
		_, err := runFirewallCommand(ctx, b.offline, "--zone="+b.zone, "--remove-port="+spec)
		return err
	}
	if _, err := runFirewallCommand(ctx, b.binary, "--permanent", "--zone="+b.zone, "--remove-port="+spec); err != nil {
		return err
	}
	_, err := runFirewallCommand(ctx, b.binary, "--zone="+b.zone, "--remove-port="+spec)
	return err
}

func setFirewallBackendEnabled(ctx context.Context, b firewallBackend, enabled bool) error {
	if b.name == "ufw" {
		args := []string{"disable"}
		if enabled {
			args = []string{"--force", "enable"}
		}
		_, err := runFirewallCommand(ctx, b.binary, args...)
		return err
	}
	systemctl, err := exec.LookPath("systemctl")
	if err != nil {
		return errors.New("systemctl is required to control firewalld")
	}
	args := []string{"disable", "--now", "firewalld"}
	if enabled {
		args = []string{"enable", "--now", "firewalld"}
	}
	_, err = runFirewallCommand(ctx, systemctl, args...)
	return err
}

func runFirewallCommand(parent context.Context, binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if ctx.Err() != nil {
		return text, fmt.Errorf("%s timed out", binary)
	}
	if err != nil {
		if text != "" {
			return text, errors.New(text)
		}
		return text, err
	}
	return text, nil
}

func detectSSHPorts() []int {
	ports := map[int]bool{}
	if sshd, err := exec.LookPath("sshd"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		out, e := runFirewallCommand(ctx, sshd, "-T")
		cancel()
		if e == nil {
			for line := range strings.SplitSeq(out, "\n") {
				f := strings.Fields(line)
				if len(f) == 2 && f[0] == "port" {
					if p, e := strconv.Atoi(f[1]); e == nil && p > 0 && p <= 65535 {
						ports[p] = true
					}
				}
			}
		}
	}
	if len(ports) == 0 {
		if raw, err := os.ReadFile("/etc/ssh/sshd_config"); err == nil {
			for line := range strings.SplitSeq(string(raw), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				f := strings.Fields(line)
				if len(f) >= 2 && strings.EqualFold(f[0], "Port") {
					if p, e := strconv.Atoi(f[1]); e == nil && p > 0 && p <= 65535 {
						ports[p] = true
					}
				}
			}
		}
	}
	if len(ports) == 0 {
		ports[22] = true
	}
	out := make([]int, 0, len(ports))
	for p := range ports {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}
