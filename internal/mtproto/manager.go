package mtproto

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

type SecretEntry struct {
	Name        string
	Secret      string
	AdTag       string
	QuotaBytes  int64
	ExpiresUnix int64
	LimitIP     int
}

type Instance struct {
	Id      int
	Tag     string
	Listen  string
	Port    int
	Secrets []SecretEntry

	Debug                     bool
	ProxyProtocolListener     bool
	PreferIP                  string
	ThrottleMaxConnections    int
	PublicIPv4                string
	PublicIPv6                string
	RouteThroughXray          bool
	XrayRoutePort             int
	FakeTLSDomain             string
	FakeTLSDomains            []string
	TLSMask                   *bool
	AllowLegacyModes          *bool
	TLSEmulation              *bool
	UnknownSNIAction          string
	ProxyProtocolTrustedCIDRs []string
	MaskHost                  string
	MaskPort                  int
	MaskProxyProtocol         int
}

func (inst Instance) bindTo() string {
	listen := strings.TrimSpace(inst.Listen)
	if listen == "" {
		listen = "0.0.0.0"
	}
	return fmt.Sprintf("%s:%d", listen, inst.Port)
}

func (inst Instance) structuralFingerprint() string {
	return strings.Join([]string{
		inst.bindTo(),
		strconv.FormatBool(inst.Debug),
		strconv.FormatBool(inst.ProxyProtocolListener),
		inst.PreferIP,
		strconv.Itoa(inst.ThrottleMaxConnections),
		strconv.FormatBool(inst.RouteThroughXray),
		strconv.Itoa(inst.XrayRoutePort),
		inst.PublicIPv4,
		inst.PublicIPv6,
		inst.FakeTLSDomain,
		strings.Join(inst.FakeTLSDomains, ","),
		strconv.FormatBool(inst.maskEnabled()),
		strconv.FormatBool(inst.emulationEnabled()),
		strconv.FormatBool(inst.legacyModesEnabled()),
		inst.sniAction(),
		strconv.FormatBool(inst.useMiddleProxy()),
		strings.Join(inst.ProxyProtocolTrustedCIDRs, ","),
		inst.MaskHost,
		strconv.Itoa(inst.MaskPort),
		strconv.Itoa(inst.MaskProxyProtocol),
	}, "|")
}

func (inst Instance) secretsFingerprint() string {
	pairs := make([]string, 0, len(inst.Secrets))
	for _, e := range inst.Secrets {
		pairs = append(pairs, fmt.Sprintf("%s=%s;tag=%s;q=%d;exp=%d;ip=%d", e.Name, e.Secret, e.AdTag, e.QuotaBytes, e.ExpiresUnix, e.LimitIP))
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
	up   int64
	down int64
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
	userEmails   map[string]string
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
	managerOnce.Do(func() {
		manager = &Manager{procs: map[int]*managed{}}
	})
	return manager
}

// decodeLegacySecret converts a Telegram-link MTProxy secret to Telemt's raw
// 32-hex user secret and recovers the FakeTLS host embedded after an ee secret.
func decodeLegacySecret(secret string) (raw, domain string) {
	s := strings.ToLower(strings.TrimSpace(secret))
	if len(s) >= 34 && (strings.HasPrefix(s, "ee") || strings.HasPrefix(s, "dd")) {
		raw = s[2:34]
		if strings.HasPrefix(s, "ee") && len(s) > 34 {
			if b, err := hex.DecodeString(s[34:]); err == nil {
				domain = strings.TrimSpace(string(b))
			}
		}
		return raw, domain
	}
	if len(s) >= 32 {
		return s[:32], ""
	}
	return s, ""
}

func validRawSecret(secret string) bool {
	if len(secret) != 32 {
		return false
	}
	_, err := hex.DecodeString(secret)
	return err == nil
}

func InstanceFromInbound(ib *model.Inbound) (Instance, bool) {
	if ib == nil || ib.Protocol != model.MTProto {
		return Instance{}, false
	}
	var parsed struct {
		ProxyProtocolListener     bool     `json:"proxyProtocolListener"`
		Debug                     bool     `json:"debug"`
		FakeTLSDomain             string   `json:"fakeTlsDomain"`
		TLSMask                   *bool    `json:"tlsMask"`
		AllowLegacyModes          *bool    `json:"allowLegacyModes"`
		TLSEmulation              *bool    `json:"tlsEmulation"`
		UnknownSNIAction          string   `json:"unknownSniAction"`
		ProxyProtocolTrustedCIDRs []string `json:"proxyProtocolTrustedCidrs"`
		MaskHost                  *string  `json:"maskHost"`
		MaskPort                  int      `json:"maskPort"`
		MaskProxyProtocol         int      `json:"maskProxyProtocol"`
		DomainFronting            struct {
			IP            string `json:"ip"`
			Port          int    `json:"port"`
			ProxyProtocol bool   `json:"proxyProtocol"`
		} `json:"domainFronting"`
		PreferIP               string `json:"preferIp"`
		ThrottleMaxConnections int    `json:"throttleMaxConnections"`
		RouteThroughXray       bool   `json:"routeThroughXray"`
		RouteXrayPort          int    `json:"routeXrayPort"`
		PublicIPv4             string `json:"publicIpv4"`
		PublicIPv6             string `json:"publicIpv6"`
		Clients                []struct {
			Email      string `json:"email"`
			Secret     string `json:"secret"`
			AdTag      string `json:"adTag"`
			Enable     bool   `json:"enable"`
			TotalGB    int64  `json:"totalGB"`
			ExpiryTime int64  `json:"expiryTime"`
			LimitIP    int    `json:"limitIp"`
		} `json:"clients"`
	}
	if err := json.Unmarshal([]byte(ib.Settings), &parsed); err != nil {
		return Instance{}, false
	}
	secrets := make([]SecretEntry, 0, len(parsed.Clients))
	domain := strings.TrimSpace(parsed.FakeTLSDomain)
	domains := map[string]struct{}{}
	for _, c := range parsed.Clients {
		if !c.Enable || c.Secret == "" || c.Email == "" {
			continue
		}
		raw, embeddedDomain := decodeLegacySecret(c.Secret)
		if !validRawSecret(raw) {
			continue
		}
		if domain == "" && embeddedDomain != "" {
			domain = embeddedDomain
		}
		if embeddedDomain != "" {
			domains[strings.ToLower(embeddedDomain)] = struct{}{}
		}
		e := SecretEntry{Name: c.Email, Secret: raw, AdTag: usableAdTag(c.AdTag), LimitIP: max(c.LimitIP, 0)}
		if c.TotalGB > 0 {
			e.QuotaBytes = c.TotalGB
		}
		if c.ExpiryTime > 0 {
			e.ExpiresUnix = c.ExpiryTime / 1000
		}
		secrets = append(secrets, e)
	}
	if len(secrets) == 0 {
		return Instance{}, false
	}
	// Keep the primary and additional SNI set stable across client reordering.
	additionalDomains := slices.Sorted(maps.Keys(domains))
	if domain == "" {
		domain = "www.cloudflare.com"
	} else if strings.TrimSpace(parsed.FakeTLSDomain) == "" && len(additionalDomains) > 0 {
		domain = additionalDomains[0]
	}
	domain = strings.ToLower(domain)
	additionalDomains = slices.DeleteFunc(additionalDomains, func(s string) bool { return s == domain })
	maskHost, maskPort, maskProxy := "", parsed.MaskPort, parsed.MaskProxyProtocol
	if parsed.MaskHost != nil {
		maskHost = strings.TrimSpace(*parsed.MaskHost)
	}
	if parsed.MaskHost == nil && strings.TrimSpace(parsed.DomainFronting.IP) != "" {
		maskHost, maskPort = strings.TrimSpace(parsed.DomainFronting.IP), parsed.DomainFronting.Port
		if parsed.DomainFronting.ProxyProtocol {
			maskProxy = 1
		}
	}
	if maskPort <= 0 {
		maskPort = 443
	}
	trusted := parsed.ProxyProtocolTrustedCIDRs
	if trusted == nil {
		trusted = []string{"127.0.0.0/8", "::1/128"}
	}
	return Instance{
		Id:                        ib.Id,
		Tag:                       ib.Tag,
		Listen:                    ib.Listen,
		Port:                      ib.Port,
		Secrets:                   secrets,
		Debug:                     parsed.Debug,
		ProxyProtocolListener:     parsed.ProxyProtocolListener,
		PreferIP:                  parsed.PreferIP,
		ThrottleMaxConnections:    parsed.ThrottleMaxConnections,
		RouteThroughXray:          parsed.RouteThroughXray,
		XrayRoutePort:             parsed.RouteXrayPort,
		PublicIPv4:                strings.TrimSpace(parsed.PublicIPv4),
		PublicIPv6:                strings.TrimSpace(parsed.PublicIPv6),
		FakeTLSDomain:             domain,
		FakeTLSDomains:            additionalDomains,
		TLSMask:                   parsed.TLSMask,
		AllowLegacyModes:          parsed.AllowLegacyModes,
		TLSEmulation:              parsed.TLSEmulation,
		UnknownSNIAction:          parsed.UnknownSNIAction,
		ProxyProtocolTrustedCIDRs: trusted,
		MaskHost:                  maskHost, MaskPort: maskPort, MaskProxyProtocol: maskProxy,
	}, true
}

func usableAdTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if !model.ValidMtprotoAdTag(tag) {
		return ""
	}
	return tag
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
	if n := killStrayTelemtSidecars(GetBinaryPath()); n > 0 {
		logger.Warningf("mtproto: terminated %d orphaned Telemt process(es)", n)
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

// stopManaged treats an already-exited process as stopped, but never lets the
// manager forget a sidecar that Stop failed to terminate. Keeping the entry and
// its config makes the next reconcile retry the stop instead of starting a
// second Telemt process on the same listener.
func stopManaged(cur *managed) error {
	if cur == nil || cur.proc == nil || !cur.proc.IsRunning() {
		return nil
	}
	if err := cur.proc.Stop(); err != nil && cur.proc.IsRunning() {
		return err
	}
	return nil
}

func (m *Manager) ensureLocked(inst Instance) error {
	structFP, secFP := inst.structuralFingerprint(), inst.secretsFingerprint()
	if cur, ok := m.procs[inst.Id]; ok {
		switch ensureActionFor(cur.proc.IsRunning(), cur.structuralFP, cur.secretsFP, structFP, secFP) {
		case ensureNoop:
			cur.tag = inst.Tag
			cur.userEmails = nativeUserEmails(inst.Secrets)
			return nil
		case ensureReload:
			if err := writeConfig(configPathForID(inst.Id), inst, cur.apiPort, cur.apiToken); err != nil {
				return err
			}
			if reloadTelemt(cur.apiPort, cur.apiToken) {
				cur.tag = inst.Tag
				cur.secretsFP = secFP
				cur.userEmails = nativeUserEmails(inst.Secrets)
				logger.Infof("mtproto: reloaded Telemt users for inbound %d", inst.Id)
				return nil
			}
			logger.Warningf("mtproto: Telemt reload failed for inbound %d, restarting", inst.Id)
			fallthrough
		case ensureRestart:
			if err := stopManaged(cur); err != nil {
				return fmt.Errorf("mtproto: stop Telemt for inbound %d before restart: %w", inst.Id, err)
			}
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
	cfg := configPathForID(inst.Id)
	if err := writeConfig(cfg, inst, apiPort, apiToken); err != nil {
		return err
	}
	proc := newProcess(cfg, fmt.Sprintf("inbound %d", inst.Id))
	if err := proc.Start(); err != nil {
		return err
	}
	m.procs[inst.Id] = &managed{
		proc:         proc,
		tag:          inst.Tag,
		structuralFP: structFP,
		secretsFP:    secFP,
		apiPort:      apiPort,
		apiToken:     apiToken,
		last:         map[string]clientCounters{},
		userEmails:   nativeUserEmails(inst.Secrets),
	}
	logger.Infof("mtproto: started Telemt for inbound %d on %s", inst.Id, inst.bindTo())
	return nil
}

func (m *Manager) Remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.procs[id]; ok {
		if err := stopManaged(cur); err != nil {
			logger.Errorf("mtproto: failed to stop Telemt for inbound %d: %v", id, err)
			return
		}
		delete(m.procs, id)
		logger.Infof("mtproto: stopped Telemt for inbound %d", id)
	}
	if err := os.Remove(configPathForID(id)); err == nil {
		scheduleMekoSync()
	}
}

func configIDFromPath(path string) (int, bool) {
	name := filepath.Base(path)
	if !strings.HasPrefix(name, "telemt-") || !strings.HasSuffix(name, ".toml") {
		return 0, false
	}
	rawID := strings.TrimSuffix(strings.TrimPrefix(name, "telemt-"), ".toml")
	id, err := strconv.Atoi(rawID)
	return id, err == nil && id > 0
}

func removeStaleConfigs(want map[int]struct{}) int {
	files, err := filepath.Glob(filepath.Join(configDir(), "telemt-*.toml"))
	if err != nil {
		return 0
	}
	removed := 0
	for _, path := range files {
		id, ok := configIDFromPath(path)
		if !ok {
			continue
		}
		if _, keep := want[id]; keep {
			continue
		}
		if err := os.Remove(path); err == nil {
			removed++
		}
	}
	return removed
}

func (m *Manager) Reconcile(desired []Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepOrphansLocked()
	want := make(map[int]struct{}, len(desired))
	for _, i := range desired {
		want[i.Id] = struct{}{}
	}
	for id, cur := range m.procs {
		if _, ok := want[id]; !ok {
			if err := stopManaged(cur); err != nil {
				logger.Warningf("mtproto: failed to stop removed inbound %d: %v", id, err)
				continue
			}
			delete(m.procs, id)
			_ = os.Remove(configPathForID(id))
		}
	}
	if n := removeStaleConfigs(want); n > 0 {
		logger.Infof("mtproto: removed %d stale Telemt config(s)", n)
		scheduleMekoSync()
	}
	for _, i := range desired {
		if err := m.ensureLocked(i); err != nil {
			logger.Warningf("mtproto: reconcile failed for inbound %d: %v", i.Id, err)
		}
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cur := range m.procs {
		if err := stopManaged(cur); err != nil {
			logger.Warningf("mtproto: failed to stop Telemt for inbound %d during shutdown: %v", id, err)
			continue
		}
		_ = os.Remove(configPathForID(id))
		delete(m.procs, id)
	}
}

func (m *Manager) CollectTraffic() ([]Traffic, []string) {
	return m.CollectTrafficConsistent()
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
	if email == "" {
		return
	}
	type target struct {
		port  int
		token string
	}
	m.mu.Lock()
	ts := make([]target, 0, len(m.procs))
	for _, cur := range m.procs {
		if cur.proc != nil && cur.proc.IsRunning() {
			ts = append(ts, target{port: cur.apiPort, token: cur.apiToken})
		}
	}
	m.mu.Unlock()
	for _, t := range ts {
		resetQuota(t.port, t.token, email)
	}
}

func resetQuota(port int, token, email string) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/v1/users/%s/reset-quota", port, url.PathEscape(nativeUsername(email))), nil)
	if err != nil {
		return
	}
	authorize(req, token)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
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

func tomlQuote(s string) string { return strconv.Quote(s) }

func networkOptions(preferIP string) (ipv4, ipv6 bool, prefer int) {
	ipv4, ipv6, prefer = true, true, 4
	switch strings.ToLower(strings.TrimSpace(preferIP)) {
	case "prefer-ipv6":
		prefer = 6
	case "only-ipv6":
		ipv4, ipv6, prefer = false, true, 6
	case "only-ipv4":
		ipv4, ipv6, prefer = true, false, 4
	}
	return
}

func validAnnounceIP(value string, wantV6 bool) string {
	value = strings.Trim(strings.TrimSpace(value), "[]")
	ip := net.ParseIP(value)
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
		return ""
	}
	if wantV6 {
		if ip.To4() != nil {
			return ""
		}
	} else if ip.To4() == nil {
		return ""
	}
	return value
}

func (inst Instance) listenerAnnounceIP(listen string) string {
	listen = strings.Trim(strings.TrimSpace(listen), "[]")
	ip := net.ParseIP(listen)
	if ip != nil && ip.To4() == nil {
		return validAnnounceIP(inst.PublicIPv6, true)
	}
	return validAnnounceIP(inst.PublicIPv4, false)
}

func (inst Instance) maskEnabled() bool      { return inst.TLSMask == nil || *inst.TLSMask }
func (inst Instance) emulationEnabled() bool { return inst.TLSEmulation == nil || *inst.TLSEmulation }
func (inst Instance) legacyModesEnabled() bool {
	return inst.AllowLegacyModes == nil || *inst.AllowLegacyModes
}

func (inst Instance) sniAction() string {
	switch inst.UnknownSNIAction {
	case "drop", "accept", "reject_handshake":
		return inst.UnknownSNIAction
	default:
		return "mask"
	}
}

// Sponsored-channel tags are delivered by Telegram's middle proxies. Enable
// them automatically for tagged users, while preserving the local SOCKS route.
func (inst Instance) useMiddleProxy() bool {
	if inst.RouteThroughXray {
		return false
	}
	return slices.ContainsFunc(inst.Secrets, func(e SecretEntry) bool { return e.AdTag != "" })
}

func renderConfig(inst Instance, apiPort int, apiToken string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[general]\ndata_path = %s\nfast_mode = true\nuse_middle_proxy = %t\n", tomlQuote(filepath.Join(configDir(), "state", strconv.Itoa(inst.Id))), inst.useMiddleProxy())
	if inst.Debug {
		b.WriteString("log_level = \"debug\"\n")
	} else {
		b.WriteString("log_level = \"normal\"\n")
	}

	// Telemt can serve all MTProxy handshakes from the same raw 32-hex user
	// secret. Keep all three modes enabled so existing classic, dd (secure) and
	// ee (FakeTLS) Telegram links continue to work after migration from mtg.
	fmt.Fprintf(&b, "\n[general.modes]\nclassic = %t\nsecure = %t\ntls = true\n", inst.legacyModesEnabled(), inst.legacyModesEnabled())
	// The panel generates share links. Do not print user secrets into its logs.
	b.WriteString("\n[general.links]\nshow = []\n")
	ipv4, ipv6, prefer := networkOptions(inst.PreferIP)
	fmt.Fprintf(&b, "\n[network]\nipv4 = %t\nipv6 = %t\nprefer = %d\n", ipv4, ipv6, prefer)
	fmt.Fprintf(&b, "\n[server]\nport = %d\n", inst.Port)
	if inst.ProxyProtocolListener {
		trusted := inst.ProxyProtocolTrustedCIDRs
		if trusted == nil {
			trusted = []string{"127.0.0.0/8", "::1/128"}
		}
		quoted := make([]string, 0, len(trusted))
		for _, cidr := range trusted {
			quoted = append(quoted, tomlQuote(cidr))
		}
		fmt.Fprintf(&b, "proxy_protocol_trusted_cidrs = [%s]\n", strings.Join(quoted, ", "))
	}
	listen := strings.TrimSpace(inst.Listen)
	if listen == "" {
		listen = "0.0.0.0"
	}
	fmt.Fprintf(&b, "\n[[server.listeners]]\nip = %s\nproxy_protocol = %t\n", tomlQuote(listen), inst.ProxyProtocolListener)
	if announceIP := inst.listenerAnnounceIP(listen); announceIP != "" {
		fmt.Fprintf(&b, "announce_ip = %s\n", tomlQuote(announceIP))
	}
	fmt.Fprintf(&b, "\n[server.api]\nenabled = true\nlisten = %s\nwhitelist = [\"127.0.0.0/8\"]\nauth_header = %s\nread_only = false\n", tomlQuote(fmt.Sprintf("127.0.0.1:%d", apiPort)), tomlQuote("Bearer "+apiToken))
	if inst.FakeTLSDomain != "" {
		fmt.Fprintf(&b, "\n[censorship]\ntls_domain = %s\nmask = %t\ntls_emulation = %t\nunknown_sni_action = %s\n", tomlQuote(inst.FakeTLSDomain), inst.maskEnabled(), inst.emulationEnabled(), tomlQuote(inst.sniAction()))
		if !inst.emulationEnabled() {
			// Telemt randomizes the default 2048 length on every config load
			// without emulation, making a user-only reload appear structural.
			b.WriteString("fake_cert_len = 2049\n")
		}
		if inst.MaskHost != "" {
			fmt.Fprintf(&b, "mask_host = %s\n", tomlQuote(inst.MaskHost))
		}
		if inst.MaskPort > 0 {
			fmt.Fprintf(&b, "mask_port = %d\n", inst.MaskPort)
		}
		fmt.Fprintf(&b, "mask_proxy_protocol = %d\n", inst.MaskProxyProtocol)
		if len(inst.FakeTLSDomains) > 0 {
			quoted := make([]string, 0, len(inst.FakeTLSDomains))
			for _, domain := range inst.FakeTLSDomains {
				quoted = append(quoted, tomlQuote(domain))
			}
			fmt.Fprintf(&b, "tls_domains = [%s]\n", strings.Join(quoted, ", "))
		}
	}
	b.WriteString("\n[access]\nreplay_check_len = 65536\nignore_time_skew = false\n")
	if inst.ThrottleMaxConnections > 0 {
		fmt.Fprintf(&b, "user_max_tcp_conns_global_each = %d\n", inst.ThrottleMaxConnections)
	}
	b.WriteString("\n[access.users]\n")
	for _, e := range inst.Secrets {
		fmt.Fprintf(&b, "%s = %s\n", tomlQuote(nativeUsername(e.Name)), tomlQuote(e.Secret))
	}
	tagged := false
	for _, e := range inst.Secrets {
		if e.AdTag != "" {
			if !tagged {
				b.WriteString("\n[access.user_ad_tags]\n")
				tagged = true
			}
			fmt.Fprintf(&b, "%s = %s\n", tomlQuote(nativeUsername(e.Name)), tomlQuote(e.AdTag))
		}
	}
	quota := false
	for _, e := range inst.Secrets {
		if e.QuotaBytes > 0 {
			if !quota {
				b.WriteString("\n[access.user_data_quota]\n")
				quota = true
			}
			fmt.Fprintf(&b, "%s = %d\n", tomlQuote(nativeUsername(e.Name)), e.QuotaBytes)
		}
	}
	exp := false
	for _, e := range inst.Secrets {
		if e.ExpiresUnix > 0 {
			if !exp {
				b.WriteString("\n[access.user_expirations]\n")
				exp = true
			}
			fmt.Fprintf(&b, "%s = %s\n", tomlQuote(nativeUsername(e.Name)), tomlQuote(expiresString(e.ExpiresUnix)))
		}
	}
	ipLimits := false
	for _, e := range inst.Secrets {
		if e.LimitIP > 0 {
			if !ipLimits {
				b.WriteString("\n[access.user_max_unique_ips]\n")
				ipLimits = true
			}
			fmt.Fprintf(&b, "%s = %d\n", tomlQuote(nativeUsername(e.Name)), e.LimitIP)
		}
	}
	if inst.RouteThroughXray && inst.XrayRoutePort > 0 {
		fmt.Fprintf(&b, "\n[[upstreams]]\ntype = \"socks5\"\naddress = %s\nweight = 1\nenabled = true\n", tomlQuote(fmt.Sprintf("127.0.0.1:%d", inst.XrayRoutePort)))
	} else {
		b.WriteString("\n[[upstreams]]\ntype = \"direct\"\nweight = 1\nenabled = true\n")
	}
	return b.String()
}

func writeConfig(path string, inst Instance, apiPort int, apiToken string) error {
	if err := os.MkdirAll(configDir(), 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(configDir(), "state", strconv.Itoa(inst.Id)), 0o700); err != nil {
		return err
	}
	// Reload reads this file concurrently; replace it only after a full write.
	f, err := os.CreateTemp(filepath.Dir(path), ".telemt-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(renderConfig(inst, apiPort, apiToken)); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func expiresString(unix int64) string { return time.Unix(unix, 0).UTC().Format(time.RFC3339) }

func newAPIToken() (string, error) {
	buf := make([]byte, 16)
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

type telemtReloadEnvelope struct {
	OK   bool `json:"ok"`
	Data struct {
		ReloadID uint64 `json:"reload_id"`
		State    string `json:"state"`
	} `json:"data"`
}

func reloadTelemt(port int, token string) bool {
	// Current Telemt reloads prepare a fresh runtime, including network probes
	// that can take ten seconds. A five-second deadline restarts a healthy
	// sidecar while its accepted native reload is still in progress.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 4 * time.Second}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/system/reload", nil)
	if err != nil {
		return false
	}
	authorize(req, token)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	var accepted telemtReloadEnvelope
	decodeErr := json.NewDecoder(resp.Body).Decode(&accepted)
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusOK && decodeErr != nil {
		// Compatibility with old Telemt builds where reload was synchronous and
		// returned an empty 200 response.
		return true
	}
	if (resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK) || decodeErr != nil || !accepted.OK {
		return false
	}
	if accepted.Data.ReloadID == 0 {
		return resp.StatusCode == http.StatusOK
	}

	statusURL := fmt.Sprintf("%s/v1/system/reload/%d", baseURL, accepted.Data.ReloadID)
	for {
		statusReq, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
		if err != nil {
			return false
		}
		authorize(statusReq, token)
		statusResp, err := client.Do(statusReq)
		if err != nil {
			return false
		}
		var status telemtReloadEnvelope
		decodeErr := json.NewDecoder(statusResp.Body).Decode(&status)
		_ = statusResp.Body.Close()
		if statusResp.StatusCode != http.StatusOK || decodeErr != nil || !status.OK {
			return false
		}
		switch strings.ToLower(status.Data.State) {
		case "succeeded":
			return true
		case "failed", "rolled_back":
			return false
		case "accepted", "preparing", "activating", "draining":
		default:
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

type statsUser struct {
	CurrentConnections int64 `json:"current_connections"`
	TotalOctets        int64 `json:"total_octets"`
}

func scrapeStats(port int, token string) (map[string]statsUser, bool) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/v1/stats/users", port), nil)
	if err != nil {
		return nil, false
	}
	authorize(req, token)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var env struct {
		OK   bool `json:"ok"`
		Data []struct {
			Username           string `json:"username"`
			CurrentConnections int64  `json:"current_connections"`
			TotalOctets        int64  `json:"total_octets"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, false
	}
	out := make(map[string]statsUser, len(env.Data))
	for _, u := range env.Data {
		out[u.Username] = statsUser{CurrentConnections: u.CurrentConnections, TotalOctets: u.TotalOctets}
	}
	return out, true
}
