package mtproto

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

// SecretEntry is one panel client exposed to Telemt. Secret may be a legacy
// 3x-ui ee/dd-prefixed MTProxy secret; renderConfig strips the transport prefix
// and FakeTLS-domain suffix because Telemt stores the 32-hex user secret and
// generates Classic/Secure/FakeTLS links from it.
type SecretEntry struct {
	Name         string
	Secret       string
	AdTag        string
	QuotaBytes   int64
	ExpiresUnix  int64
	MaxUniqueIPs int
	MaxTCPConns  int
	RateUpBps    int64
	RateDownBps  int64
}

type TelemtModes struct {
	Classic bool
	Secure  bool
	TLS     bool
}

// MekoFixConfig controls the host-level SYN filter from MTPROTO_FIX_By_MEKO.
// It is deliberately scoped to the inbound port and owned by x-ui so unrelated
// firewall rules are never flushed or replaced.
type MekoFixConfig struct {
	Enabled          bool
	Backend          string
	SynRatePerMinute int
	Burst            int
	IOSBypass        bool
}

// Instance is the desired runtime configuration of one mtproto inbound. 3x-ui
// runs one Telemt process per inbound so existing process supervision, traffic
// jobs and node reconciliation keep the same external contract as mtg-multi.
type Instance struct {
	Id      int
	Tag     string
	Listen  string
	Port    int
	Secrets []SecretEntry

	Debug                     bool
	ProxyProtocolListener     bool
	ProxyProtocolTrustedCIDRs []string
	PreferIP                  string

	// Kept for wire compatibility with installations that still have the old
	// mtg settings saved. Telemt does not consume domain-fronting/throttle knobs.
	FrontingIP             string
	FrontingPort           int
	FrontingProxyProtocol  bool
	ThrottleMaxConnections int
	PublicIPv4             string
	PublicIPv6             string

	RouteThroughXray bool
	XrayRoutePort    int

	Modes        TelemtModes
	TLSDomain    string
	TLSDomains   []string
	Mask         bool
	TLSEmulation bool
	MekoFix      MekoFixConfig
}

func (inst Instance) bindTo() string {
	listen := strings.TrimSpace(inst.Listen)
	if listen == "" {
		listen = "0.0.0.0"
	}
	return net.JoinHostPort(strings.Trim(listen, "[]"), strconv.Itoa(inst.Port))
}

func (inst Instance) structuralFingerprint() string {
	domains := append([]string(nil), inst.TLSDomains...)
	slices.Sort(domains)
	parts := []string{
		inst.bindTo(),
		strconv.FormatBool(inst.Debug),
		strconv.FormatBool(inst.ProxyProtocolListener),
		strings.Join(inst.ProxyProtocolTrustedCIDRs, ","),
		inst.PreferIP,
		strconv.FormatBool(inst.RouteThroughXray),
		strconv.Itoa(inst.XrayRoutePort),
		strconv.FormatBool(inst.Modes.Classic),
		strconv.FormatBool(inst.Modes.Secure),
		strconv.FormatBool(inst.Modes.TLS),
		inst.TLSDomain,
		strings.Join(domains, ","),
		strconv.FormatBool(inst.Mask),
		strconv.FormatBool(inst.TLSEmulation),
		inst.PublicIPv4,
		inst.PublicIPv6,
	}
	return strings.Join(parts, "|")
}

func (inst Instance) secretsFingerprint() string {
	pairs := make([]string, 0, len(inst.Secrets))
	for _, e := range inst.Secrets {
		pairs = append(pairs, fmt.Sprintf("%s=%s;tag=%s;q=%d;exp=%d;ip=%d;tcp=%d;up=%d;down=%d",
			e.Name, e.Secret, e.AdTag, e.QuotaBytes, e.ExpiresUnix, e.MaxUniqueIPs, e.MaxTCPConns, e.RateUpBps, e.RateDownBps))
	}
	slices.Sort(pairs)
	return strings.Join(pairs, "|")
}

type Traffic struct {
	Tag   string
	Email string
	Up    int64
	Down  int64
}

type clientCounters struct {
	total int64
}

func monotonicCounterDelta(current, previous int64) int64 {
	if current <= 0 {
		return 0
	}
	if current >= previous {
		return current - previous
	}
	return current
}

type managed struct {
	proc         *Process
	tag          string
	structuralFP string
	secretsFP    string
	apiPort      int
	apiToken     string
	last         map[string]clientCounters
	users        map[string]string // Telemt username -> panel email
	mekoFix      MekoFixConfig
}

type Manager struct {
	mu    sync.Mutex
	procs map[int]*managed
	swept bool
}

var (
	managerOnce sync.Once
	manager     *Manager
)

func GetManager() *Manager {
	managerOnce.Do(func() { manager = &Manager{procs: map[int]*managed{}} })
	return manager
}

func InstanceFromInbound(ib *model.Inbound) (Instance, bool) {
	if ib == nil || ib.Protocol != model.MTProto {
		return Instance{}, false
	}
	var parsed struct {
		FakeTLSDomain             string   `json:"fakeTlsDomain"`
		TLSDomains                []string `json:"tlsDomains"`
		ProxyProtocolListener     bool     `json:"proxyProtocolListener"`
		ProxyProtocolTrustedCIDRs []string `json:"proxyProtocolTrustedCidrs"`
		Debug                     bool     `json:"debug"`
		PreferIP                  string   `json:"preferIp"`
		RouteThroughXray          bool     `json:"routeThroughXray"`
		RouteXrayPort             int      `json:"routeXrayPort"`
		PublicIPv4                string   `json:"publicIpv4"`
		PublicIPv6                string   `json:"publicIpv6"`
		Mask                      *bool    `json:"mask"`
		TLSEmulation              *bool    `json:"tlsEmulation"`
		DomainFronting            struct {
			IP            string `json:"ip"`
			Port          int    `json:"port"`
			ProxyProtocol bool   `json:"proxyProtocol"`
		} `json:"domainFronting"`
		ThrottleMaxConnections int `json:"throttleMaxConnections"`
		TelemtModes            *struct {
			Classic bool `json:"classic"`
			Secure  bool `json:"secure"`
			TLS     bool `json:"tls"`
		} `json:"telemtModes"`
		MekoFix *struct {
			Enabled          bool   `json:"enabled"`
			Backend          string `json:"backend"`
			SynRatePerMinute int    `json:"synRatePerMinute"`
			Burst            int    `json:"burst"`
			IOSBypass        *bool  `json:"iosBypass"`
		} `json:"mekoFix"`
		Clients []struct {
			Email       string `json:"email"`
			Secret      string `json:"secret"`
			AdTag       string `json:"adTag"`
			Enable      bool   `json:"enable"`
			TotalGB     int64  `json:"totalGB"`
			ExpiryTime  int64  `json:"expiryTime"`
			LimitIP     int    `json:"limitIp"`
			MaxTCPConns int    `json:"maxTcpConns"`
			RateUpBps   int64  `json:"rateLimitUpBps"`
			RateDownBps int64  `json:"rateLimitDownBps"`
		} `json:"clients"`
	}
	if err := json.Unmarshal([]byte(ib.Settings), &parsed); err != nil {
		return Instance{}, false
	}

	secrets := make([]SecretEntry, 0, len(parsed.Clients))
	for _, c := range parsed.Clients {
		raw, ok := telemtSecret(c.Secret)
		if !c.Enable || strings.TrimSpace(c.Email) == "" || !ok {
			continue
		}
		entry := SecretEntry{
			Name: c.Email, Secret: raw, AdTag: usableAdTag(c.AdTag), QuotaBytes: c.TotalGB,
			MaxUniqueIPs: c.LimitIP, MaxTCPConns: c.MaxTCPConns, RateUpBps: c.RateUpBps, RateDownBps: c.RateDownBps,
		}
		if c.ExpiryTime > 0 {
			entry.ExpiresUnix = c.ExpiryTime / 1000
		}
		secrets = append(secrets, entry)
	}
	if len(secrets) == 0 {
		return Instance{}, false
	}

	modes := TelemtModes{TLS: true}
	if parsed.TelemtModes != nil {
		modes = TelemtModes{Classic: parsed.TelemtModes.Classic, Secure: parsed.TelemtModes.Secure, TLS: parsed.TelemtModes.TLS}
		if !modes.Classic && !modes.Secure && !modes.TLS {
			modes.TLS = true
		}
	}
	mask, tlsEmulation := true, true
	if parsed.Mask != nil {
		mask = *parsed.Mask
	}
	if parsed.TLSEmulation != nil {
		tlsEmulation = *parsed.TLSEmulation
	}
	domain := cleanTLSDomain(parsed.FakeTLSDomain)
	if domain == "" {
		domain = "www.cloudflare.com"
	}
	tlsDomains := uniqueDomains(parsed.TLSDomains, domain)

	fix := MekoFixConfig{Backend: "auto", SynRatePerMinute: 54, Burst: 1, IOSBypass: true}
	if parsed.MekoFix != nil {
		fix.Enabled = parsed.MekoFix.Enabled
		if v := strings.ToLower(strings.TrimSpace(parsed.MekoFix.Backend)); v == "iptables" || v == "nftables" || v == "auto" {
			fix.Backend = v
		}
		if parsed.MekoFix.SynRatePerMinute > 0 {
			fix.SynRatePerMinute = parsed.MekoFix.SynRatePerMinute
		}
		if parsed.MekoFix.Burst > 0 {
			fix.Burst = parsed.MekoFix.Burst
		}
		if parsed.MekoFix.IOSBypass != nil {
			fix.IOSBypass = *parsed.MekoFix.IOSBypass
		}
	}

	return Instance{
		Id: ib.Id, Tag: ib.Tag, Listen: ib.Listen, Port: ib.Port, Secrets: secrets,
		Debug: parsed.Debug, ProxyProtocolListener: parsed.ProxyProtocolListener, ProxyProtocolTrustedCIDRs: cleanCIDRs(parsed.ProxyProtocolTrustedCIDRs), PreferIP: parsed.PreferIP,
		FrontingIP: parsed.DomainFronting.IP, FrontingPort: parsed.DomainFronting.Port,
		FrontingProxyProtocol: parsed.DomainFronting.ProxyProtocol, ThrottleMaxConnections: parsed.ThrottleMaxConnections,
		RouteThroughXray: parsed.RouteThroughXray, XrayRoutePort: parsed.RouteXrayPort,
		PublicIPv4: strings.TrimSpace(parsed.PublicIPv4), PublicIPv6: strings.TrimSpace(parsed.PublicIPv6),
		Modes: modes, TLSDomain: domain, TLSDomains: tlsDomains, Mask: mask, TLSEmulation: tlsEmulation, MekoFix: fix,
	}, true
}

func cleanCIDRs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(value); err != nil {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func usableAdTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if !model.ValidMtprotoAdTag(tag) {
		return ""
	}
	return tag
}

func telemtSecret(secret string) (string, bool) {
	s := strings.TrimSpace(secret)
	if len(s) >= 34 && (strings.EqualFold(s[:2], "ee") || strings.EqualFold(s[:2], "dd")) {
		s = s[2:34]
	} else if len(s) >= 32 {
		s = s[:32]
	}
	if len(s) != 32 {
		return "", false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return "", false
		}
	}
	return strings.ToLower(s), true
}

func telemtUsername(email string) string {
	s := strings.TrimSpace(email)
	if len(s) > 0 && len(s) <= 64 {
		valid := true
		for _, c := range s {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
				valid = false
				break
			}
		}
		if valid {
			return s
		}
	}
	sum := sha256.Sum256([]byte(s))
	return "u_" + hex.EncodeToString(sum[:12])
}

func cleanTLSDomain(v string) string {
	v = strings.TrimSpace(strings.Trim(v, `"`))
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	} else if i := strings.LastIndex(v, ":"); i > 0 && !strings.Contains(v, "]") {
		if _, err := strconv.Atoi(v[i+1:]); err == nil {
			v = v[:i]
		}
	}
	return strings.TrimSpace(strings.Trim(v, "[]"))
}

func uniqueDomains(values []string, primary string) []string {
	seen := map[string]struct{}{}
	if primary != "" {
		seen[strings.ToLower(primary)] = struct{}{}
	}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := cleanTLSDomain(raw)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return out
}

func (m *Manager) Ensure(inst Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepOrphansLocked()
	return m.ensureLocked(inst)
}

func (m *Manager) sweepOrphansLocked() {
	if m.swept {
		return
	}
	m.swept = true
	if n := killStrayMtgProcesses(GetBinaryPath()); n > 0 {
		logger.Warningf("mtproto: terminated %d orphaned Telemt process(es) from a previous run", n)
	}
}

type ensureAction int

const (
	ensureNoop ensureAction = iota
	ensureReload
	ensureRestart
)

func ensureActionFor(running bool, curStructFP, curSecretsFP, newStructFP, newSecretsFP string) ensureAction {
	if !running || curStructFP != newStructFP {
		return ensureRestart
	}
	if curSecretsFP != newSecretsFP {
		return ensureReload
	}
	return ensureNoop
}

func (m *Manager) ensureLocked(inst Instance) error {
	structFP, secFP := inst.structuralFingerprint(), inst.secretsFingerprint()
	if cur, ok := m.procs[inst.Id]; ok {
		switch ensureActionFor(cur.proc.IsRunning(), cur.structuralFP, cur.secretsFP, structFP, secFP) {
		case ensureNoop:
			cur.tag = inst.Tag
			cur.users = runtimeUserMap(inst)
			cur.mekoFix = inst.MekoFix
			m.applyMekoFixLocked(inst)
			return nil
		case ensureReload:
			if err := writeConfig(configPathForID(inst.Id), inst, cur.apiPort, cur.apiToken); err != nil {
				return err
			}
			if requestReload(cur.apiPort, cur.apiToken) {
				cur.tag = inst.Tag
				cur.secretsFP = secFP
				cur.users = runtimeUserMap(inst)
				cur.mekoFix = inst.MekoFix
				m.applyMekoFixLocked(inst)
				logger.Infof("mtproto: applied Telemt user update to inbound %d in place", inst.Id)
				return nil
			}
			logger.Warningf("mtproto: Telemt runtime reload unavailable for inbound %d, restarting", inst.Id)
			fallthrough
		case ensureRestart:
			_ = cur.proc.Stop()
			removeMekoFix(inst.Id)
			delete(m.procs, inst.Id)
		}
	}

	apiPort, err := FreeLocalPort()
	if err != nil {
		return err
	}
	apiToken, err := newAPIToken()
	if err != nil {
		return err
	}
	cfgPath := configPathForID(inst.Id)
	if err := writeConfig(cfgPath, inst, apiPort, apiToken); err != nil {
		return err
	}
	proc := newProcess(cfgPath, fmt.Sprintf("inbound %d", inst.Id))
	if err := proc.Start(); err != nil {
		return err
	}
	m.procs[inst.Id] = &managed{
		proc: proc, tag: inst.Tag, structuralFP: structFP, secretsFP: secFP,
		apiPort: apiPort, apiToken: apiToken, last: map[string]clientCounters{},
		users: runtimeUserMap(inst), mekoFix: inst.MekoFix,
	}
	m.applyMekoFixLocked(inst)
	logger.Infof("mtproto: started Telemt for inbound %d on %s", inst.Id, inst.bindTo())
	return nil
}

func (m *Manager) applyMekoFixLocked(inst Instance) {
	if err := applyMekoFix(inst.Id, inst.Port, inst.MekoFix); err != nil {
		logger.Warningf("mtproto: MEKO fix for inbound %d: %v", inst.Id, err)
	}
}

func (m *Manager) Remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.procs[id]; ok {
		_ = cur.proc.Stop()
		delete(m.procs, id)
	}
	removeMekoFix(id)
	_ = os.Remove(configPathForID(id))
	logger.Infof("mtproto: stopped Telemt for inbound %d", id)
}

func (m *Manager) Reconcile(desired []Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepOrphansLocked()
	want := make(map[int]struct{}, len(desired))
	for _, inst := range desired {
		want[inst.Id] = struct{}{}
	}
	for id, cur := range m.procs {
		if _, ok := want[id]; !ok {
			_ = cur.proc.Stop()
			delete(m.procs, id)
			removeMekoFix(id)
			_ = os.Remove(configPathForID(id))
		}
	}
	for _, inst := range desired {
		if err := m.ensureLocked(inst); err != nil {
			logger.Warningf("mtproto: reconcile failed for inbound %d: %v", inst.Id, err)
		}
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cur := range m.procs {
		_ = cur.proc.Stop()
		removeMekoFix(id)
		_ = os.Remove(configPathForID(id))
		delete(m.procs, id)
	}
}

func (m *Manager) CollectTraffic() ([]Traffic, []string) {
	type snap struct {
		id       int
		apiPort  int
		apiToken string
		tag      string
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
		snaps = append(snaps, snap{id: id, apiPort: cur.apiPort, apiToken: cur.apiToken, tag: cur.tag, last: lastCopy, users: usersCopy})
	}
	m.mu.Unlock()

	var out []Traffic
	var online []string
	for _, s := range snaps {
		users, ok := scrapeStats(s.apiPort, s.apiToken)
		if !ok {
			continue
		}
		newLast := make(map[string]clientCounters, len(users))
		for username, u := range users {
			email, exists := s.users[username]
			if !exists {
				continue
			}
			total := int64(u.TotalOctets)
			newLast[username] = clientCounters{total: total}
			if u.Connections > 0 {
				online = append(online, email)
			}
			prev, had := s.last[username]
			if !had {
				continue
			}
			delta := monotonicCounterDelta(total, prev.total)
			if delta > 0 {
				// Telemt's stable per-user API exposes total_octets rather than a
				// directional split. Put the exact total delta in Down so the
				// panel's Up+Down accounting remains byte-accurate.
				out = append(out, Traffic{Tag: s.tag, Email: email, Down: delta})
			}
		}
		m.mu.Lock()
		if cur, ok := m.procs[s.id]; ok {
			cur.last = newLast
		}
		m.mu.Unlock()
	}
	return out, online
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

func (m *Manager) ResetQuota(email string) {
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	type target struct {
		port            int
		token, username string
	}
	m.mu.Lock()
	targets := make([]target, 0, len(m.procs))
	for _, cur := range m.procs {
		if cur.proc == nil || !cur.proc.IsRunning() {
			continue
		}
		for username, panelEmail := range cur.users {
			if panelEmail == email {
				targets = append(targets, target{cur.apiPort, cur.apiToken, username})
				break
			}
		}
	}
	m.mu.Unlock()
	for _, t := range targets {
		resetQuota(t.port, t.token, t.username)
	}
}

func resetQuota(port int, token, username string) {
	client := http.Client{Timeout: 3 * time.Second}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/v1/users/%s/reset-quota", port, url.PathEscape(username))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, nil)
	if err != nil {
		return
	}
	authorize(req, token)
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}

func FreeLocalPort() (int, error) {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func renderConfig(inst Instance, apiPort int, apiToken string) string {
	var b strings.Builder
	b.WriteString("[general]\nuse_middle_proxy = false\n")
	if inst.Debug {
		b.WriteString("log_level = \"debug\"\n")
	} else {
		b.WriteString("log_level = \"normal\"\n")
	}
	b.WriteString("\n[general.modes]\n")
	fmt.Fprintf(&b, "classic = %t\nsecure = %t\ntls = %t\n", inst.Modes.Classic, inst.Modes.Secure, inst.Modes.TLS)
	b.WriteString("\n[general.links]\nshow = \"*\"\n")
	if inst.PublicIPv4 != "" {
		fmt.Fprintf(&b, "public_host = %q\n", inst.PublicIPv4)
	} else if inst.PublicIPv6 != "" {
		fmt.Fprintf(&b, "public_host = %q\n", strings.Trim(inst.PublicIPv6, "[]"))
	}
	fmt.Fprintf(&b, "public_port = %d\n", inst.Port)

	fmt.Fprintf(&b, "\n[server]\nport = %d\n", inst.Port)
	if inst.ProxyProtocolListener {
		b.WriteString("proxy_protocol = true\n")
		cidrs := inst.ProxyProtocolTrustedCIDRs
		if len(cidrs) == 0 {
			cidrs = []string{"127.0.0.0/8", "::1/128"}
		}
		b.WriteString("proxy_protocol_trusted_cidrs = [")
		for i, cidr := range cidrs {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", cidr)
		}
		b.WriteString("]\n")
	}
	fmt.Fprintf(&b, "\n[server.api]\nenabled = true\nlisten = \"127.0.0.1:%d\"\nwhitelist = [\"127.0.0.1/32\", \"::1/128\"]\n", apiPort)
	if apiToken != "" {
		fmt.Fprintf(&b, "auth_header = %q\n", "Bearer "+apiToken)
	}
	b.WriteString("minimal_runtime_enabled = true\n")

	listen := strings.Trim(strings.TrimSpace(inst.Listen), "[]")
	if listen == "" {
		listen = "0.0.0.0"
	}
	b.WriteString("\n[[server.listeners]]\n")
	fmt.Fprintf(&b, "ip = %q\n", listen)

	b.WriteString("\n[censorship]\n")
	fmt.Fprintf(&b, "tls_domain = %q\nmask = %t\ntls_emulation = %t\n", inst.TLSDomain, inst.Mask, inst.TLSEmulation)
	if len(inst.TLSDomains) > 0 {
		b.WriteString("tls_domains = [")
		for i, d := range inst.TLSDomains {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", d)
		}
		b.WriteString("]\n")
	}

	if inst.PreferIP != "" {
		b.WriteString("\n[network]\n")
		switch inst.PreferIP {
		case "prefer-ipv6":
			b.WriteString("ipv4 = true\nipv6 = true\nprefer = 6\n")
		case "only-ipv6":
			b.WriteString("ipv4 = false\nipv6 = true\nprefer = 6\n")
		case "only-ipv4":
			b.WriteString("ipv4 = true\nipv6 = false\nprefer = 4\n")
		default:
			b.WriteString("ipv4 = true\nipv6 = true\nprefer = 4\n")
		}
	}
	if inst.RouteThroughXray && inst.XrayRoutePort > 0 {
		b.WriteString("\n[[upstreams]]\ntype = \"socks5\"\n")
		fmt.Fprintf(&b, "address = \"127.0.0.1:%d\"\n", inst.XrayRoutePort)
	}

	writeAccessTables(&b, inst)
	return b.String()
}

func writeAccessTables(b *strings.Builder, inst Instance) {
	b.WriteString("\n[access.users]\n")
	for _, e := range inst.Secrets {
		raw, ok := telemtSecret(e.Secret)
		if !ok {
			continue
		}
		fmt.Fprintf(b, "%q = %q\n", telemtUsername(e.Name), raw)
	}
	writeStringMap := func(header string, value func(SecretEntry) string) {
		wrote := false
		for _, e := range inst.Secrets {
			v := value(e)
			if v == "" {
				continue
			}
			if !wrote {
				fmt.Fprintf(b, "\n[%s]\n", header)
				wrote = true
			}
			fmt.Fprintf(b, "%q = %q\n", telemtUsername(e.Name), v)
		}
	}
	writeIntMap := func(header string, value func(SecretEntry) int64) {
		wrote := false
		for _, e := range inst.Secrets {
			v := value(e)
			if v <= 0 {
				continue
			}
			if !wrote {
				fmt.Fprintf(b, "\n[%s]\n", header)
				wrote = true
			}
			fmt.Fprintf(b, "%q = %d\n", telemtUsername(e.Name), v)
		}
	}
	writeStringMap("access.user_ad_tags", func(e SecretEntry) string { return e.AdTag })
	writeStringMap("access.user_expirations", func(e SecretEntry) string {
		if e.ExpiresUnix <= 0 {
			return ""
		}
		return time.Unix(e.ExpiresUnix, 0).UTC().Format(time.RFC3339)
	})
	writeIntMap("access.user_data_quota", func(e SecretEntry) int64 { return e.QuotaBytes })
	writeIntMap("access.user_max_unique_ips", func(e SecretEntry) int64 { return int64(e.MaxUniqueIPs) })
	writeIntMap("access.user_max_tcp_conns", func(e SecretEntry) int64 { return int64(e.MaxTCPConns) })
	for _, e := range inst.Secrets {
		if e.RateUpBps <= 0 && e.RateDownBps <= 0 {
			continue
		}
		fmt.Fprintf(b, "\n[access.user_rate_limits.%q]\nup_bps = %d\ndown_bps = %d\n", telemtUsername(e.Name), max(e.RateUpBps, 0), max(e.RateDownBps, 0))
	}
}

func writeConfig(path string, inst Instance, apiPort int, apiToken string) error {
	if err := os.MkdirAll(configDir(), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(renderConfig(inst, apiPort, apiToken)), 0o640)
}

func runtimeUserMap(inst Instance) map[string]string {
	out := make(map[string]string, len(inst.Secrets))
	for _, e := range inst.Secrets {
		out[telemtUsername(e.Name)] = e.Name
	}
	return out
}

func newAPIToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func authorize(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func requestReload(port int, token string) bool {
	client := http.Client{Timeout: 4 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/v1/system/reload", port), nil)
	if err != nil {
		return false
	}
	authorize(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted
}

type statsUser struct {
	Username    string `json:"username"`
	Connections uint64 `json:"current_connections"`
	TotalOctets uint64 `json:"total_octets"`
}

func scrapeStats(port int, token string) (map[string]statsUser, bool) {
	client := http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/v1/users", port), nil)
	if err != nil {
		return nil, false
	}
	authorize(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var envelope struct {
		OK   bool        `json:"ok"`
		Data []statsUser `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil || !envelope.OK {
		return nil, false
	}
	out := make(map[string]statsUser, len(envelope.Data))
	for _, u := range envelope.Data {
		out[u.Username] = u
	}
	return out, true
}
