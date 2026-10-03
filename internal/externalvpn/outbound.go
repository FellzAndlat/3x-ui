package externalvpn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/google/uuid"
)

type OutboundSettings struct {
	Token      string              `json:"token"`
	MTU        int                 `json:"mtu"`
	SNI        string              `json:"sni"`
	Bypass     string              `json:"bypass"`
	Secret     string              `json:"secret"`
	Context    string              `json:"context"`
	Transports []OpenFluxTransport `json:"transports"`
}

type managedOutbound struct {
	raw         map[string]any
	fingerprint string
	port        int
	password    string
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	done        chan struct{}
	container   string
	nextRetry   time.Time
	failures    int
}

var outboundMu sync.Mutex
var outboundProcesses = map[string]*managedOutbound{}

func IsAdditionalOutbound(raw map[string]any) bool {
	p, _ := raw["protocol"].(string)
	return strings.EqualFold(p, "fptn") || strings.EqualFold(p, "openflux")
}

func ValidateAdditionalOutbound(raw map[string]any) error {
	data, err := json.Marshal(raw["settings"])
	if err != nil {
		return err
	}
	var s OutboundSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	p, _ := raw["protocol"].(string)
	if strings.EqualFold(p, "fptn") {
		if !strings.HasPrefix(s.Token, "fptn:") {
			return fmt.Errorf("FPTN outbound requires an upstream access token")
		}
		if s.MTU != 0 && (s.MTU < 576 || s.MTU > 9000) {
			return fmt.Errorf("invalid FPTN MTU")
		}
		if s.SNI != "" && !domainPattern.MatchString(s.SNI) {
			return fmt.Errorf("invalid FPTN SNI")
		}
		if s.Bypass != "" && s.Bypass != "obfuscation" && s.Bypass != "sni-spoofing" {
			return fmt.Errorf("unsupported FPTN bypass preset")
		}
		_, err := DecodeFPTNToken(s.Token)
		return err
	}
	inst := Instance{Protocol: model.OpenFlux, Port: 443, Settings: Settings{Context: s.Context, Transports: s.Transports, Clients: []Client{{Email: "outbound", Password: s.Secret, Enable: true}}}}
	if err := inst.Validate(); err != nil {
		return err
	}
	for _, t := range s.Transports {
		if t.Type == "direct" {
			host, port, err := net.SplitHostPort(t.Dial)
			n, e := strconv.Atoi(port)
			if err != nil || e != nil || host == "" || strings.ContainsAny(t.Dial, "\r\n\x00#; \t") || n < 1 || n > 65535 {
				return fmt.Errorf("OpenFlux direct outbound requires host:port")
			}
		}
	}
	return nil
}

func EnsureOutbound(raw map[string]any) (map[string]any, error) {
	if err := ValidateAdditionalOutbound(raw); err != nil {
		return nil, err
	}
	tag, _ := raw["tag"].(string)
	if strings.TrimSpace(tag) == "" {
		return nil, fmt.Errorf("managed outbound requires tag")
	}
	outboundMu.Lock()
	defer outboundMu.Unlock()
	data, _ := json.Marshal(raw)
	sum := sha256.Sum256(data)
	fingerprint := hex.EncodeToString(sum[:])
	old := outboundProcesses[tag]
	if old != nil {
		alive := true
		select {
		case <-old.done:
			alive = false
		default:
		}
		if old.fingerprint == fingerprint && alive {
			return outboundBridge(tag, old), nil
		}
		stopOutbound(old)
	}
	proc := &managedOutbound{fingerprint: fingerprint}
	if err := json.Unmarshal(data, &proc.raw); err != nil {
		return nil, err
	}
	if old != nil {
		proc.port, proc.password = old.port, old.password
	}
	if proc.port == 0 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		proc.port = listener.Addr().(*net.TCPAddr).Port
		listener.Close()
	}
	if proc.password == "" {
		proc.password = uuid.NewString()
	}
	if err := startOutbound(tag, proc); err != nil {
		delete(outboundProcesses, tag)
		return nil, err
	}
	outboundProcesses[tag] = proc
	return outboundBridge(tag, proc), nil
}

func outboundBridge(tag string, proc *managedOutbound) map[string]any {
	server := map[string]any{"address": "127.0.0.1", "port": proc.port}
	// OpenFlux exposes an unauthenticated SOCKS listener, restricted to loopback.
	p, _ := proc.raw["protocol"].(string)
	if strings.EqualFold(p, "fptn") {
		return map[string]any{"tag": tag, "protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": proc.port, "users": []any{map[string]any{"id": proc.password, "encryption": "none"}}}}}, "streamSettings": map[string]any{"network": "tcp", "security": "none"}}
	}
	return map[string]any{"tag": tag, "protocol": "socks", "settings": map[string]any{"servers": []any{server}}}
}

func startOutbound(tag string, proc *managedOutbound) error {
	p, _ := proc.raw["protocol"].(string)
	data, _ := json.Marshal(proc.raw["settings"])
	var s OutboundSettings
	_ = json.Unmarshal(data, &s)
	sum := sha256.Sum256([]byte(tag))
	suffix := hex.EncodeToString(sum[:8])
	folder := filepath.Join(directory(), "outbound-"+suffix)
	if err := os.MkdirAll(folder, 0700); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	proc.cancel = cancel
	if strings.EqualFold(p, "openflux") {
		secret := filepath.Join(folder, "secret")
		if strings.ContainsAny(secret, "\r\n#;") {
			cancel()
			return fmt.Errorf("unsupported runtime directory")
		}
		if err := writePrivate(secret, []byte(s.Secret)); err != nil {
			cancel()
			return err
		}
		var conf strings.Builder
		fmt.Fprintf(&conf, "[Interface]\nRole = client\nInbound = socks5\nSocks5 = 127.0.0.1:%d\nCodec = batched\nEncryptionKeyFile = %s\nSessionContext = %s\nCookieStore = %s\n", proc.port, secret, s.Context, filepath.Join(folder, "cookies.json"))
		for _, t := range s.Transports {
			fmt.Fprintf(&conf, "\n[Transport \"%s\"]\nType = %s\nPriority = %d\n", t.Type, t.Type, t.Priority)
			if t.Type == "direct" {
				fmt.Fprintf(&conf, "Dial = %s\n", t.Dial)
			} else {
				fmt.Fprintf(&conf, "URL = %s\n", t.URL)
			}
		}
		path := filepath.Join(folder, "client.conf")
		if err := writePrivate(path, []byte(conf.String())); err != nil {
			cancel()
			return err
		}
		proc.cmd = exec.CommandContext(ctx, binary(model.OpenFlux), "--config", path, "--negotiate")
	} else {
		checkCtx, checkCancel := context.WithTimeout(ctx, 10*time.Second)
		checkErr := exec.CommandContext(checkCtx, "docker", "image", "inspect", "3x-ui-fptn-client:0.4.6").Run()
		checkCancel()
		if checkErr != nil {
			cancel()
			return fmt.Errorf("FPTN outbound requires a running Docker daemon and the installed FPTN client image: %w", checkErr)
		}
		if err := writePrivate(filepath.Join(folder, "token"), []byte(s.Token)); err != nil {
			cancel()
			return err
		}
		if s.MTU == 0 {
			s.MTU = 1400
		}
		if s.SNI == "" {
			s.SNI = "www.bing.com"
		}
		if s.Bypass == "" {
			s.Bypass = "sni-spoofing"
		}
		// The VLESS bridge can dial only via fptn0; DNS takes the same outbound.
		// A dead tunnel therefore fails closed instead of falling back to eth0.
		socks := map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "vless", "tag": "panel-in", "listen": "0.0.0.0", "listen_port": 1080, "users": []any{map[string]any{"name": "panel", "uuid": proc.password}}}}, "outbounds": []any{map[string]any{"type": "direct", "tag": "vpn-out", "bind_interface": "fptn0"}}, "dns": map[string]any{"servers": []any{map[string]any{"type": "udp", "tag": "vpn-dns", "server": "1.1.1.1", "detour": "vpn-out"}}, "final": "vpn-dns", "strategy": "ipv4_only"}, "route": map[string]any{"final": "vpn-out", "default_domain_resolver": "vpn-dns"}}
		cfg, _ := json.Marshal(socks)
		if err := writePrivate(filepath.Join(folder, "socks.json"), cfg); err != nil {
			cancel()
			return err
		}
		script := `#!/bin/sh
set -eu
/usr/bin/fptn-client-cli --access-token "$(cat /run/panel/token)" --tun-interface-name fptn0 --mtu-size "$1" --sni "$2" --bypass-method "$3" --enable-ad-block false &
client=$!
trap 'kill "$client" 2>/dev/null || true' EXIT INT TERM
n=0
while ! ip link show fptn0 >/dev/null 2>&1; do
 kill -0 "$client" || exit 1
 n=$((n+1)); [ "$n" -le 60 ] || exit 1
 sleep 1
done
/usr/bin/panel-singbox run -c /run/panel/socks.json &
socks=$!
while kill -0 "$client" 2>/dev/null && kill -0 "$socks" 2>/dev/null; do sleep 1; done
kill "$client" "$socks" 2>/dev/null || true
exit 1
`
		if err := writePrivate(filepath.Join(folder, "run.sh"), []byte(script)); err != nil {
			cancel()
			return err
		}
		sb, err := filepath.Abs(filepath.Join(config.GetBinFolderPath(), "sing-box"))
		if err != nil {
			cancel()
			return err
		}
		if _, err := os.Stat(sb); err != nil {
			cancel()
			return fmt.Errorf("FPTN outbound requires installed sing-box: %w", err)
		}
		proc.container = containerName(0) + "-out-" + suffix
		cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", proc.container).Run()
		done()
		proc.cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "--name", proc.container, "--cap-add", "NET_ADMIN", "--device", "/dev/net/tun:/dev/net/tun", "--publish", fmt.Sprintf("127.0.0.1:%d:1080/tcp", proc.port), "--mount", "type=bind,src="+folder+",dst=/run/panel,readonly", "--mount", "type=bind,src="+sb+",dst=/usr/bin/panel-singbox,readonly", "3x-ui-fptn-client:0.4.6", "/bin/sh", "/run/panel/run.sh", strconv.Itoa(s.MTU), s.SNI, s.Bypass)
	}
	proc.cmd.Stdout, proc.cmd.Stderr = os.Stdout, os.Stderr
	if err := proc.cmd.Start(); err != nil {
		cancel()
		return err
	}
	proc.done = make(chan struct{})
	go func() { _ = proc.cmd.Wait(); close(proc.done) }()
	return nil
}

func stopOutbound(proc *managedOutbound) {
	if proc.container != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", proc.container).Run()
		cancel()
	}
	if proc.cancel != nil {
		proc.cancel()
	}
	if proc.done != nil {
		select {
		case <-proc.done:
		case <-time.After(3 * time.Second):
		}
	}
}
func KeepOutbounds(tags map[string]bool) {
	outboundMu.Lock()
	defer outboundMu.Unlock()
	for tag, proc := range outboundProcesses {
		if !tags[tag] {
			stopOutbound(proc)
			delete(outboundProcesses, tag)
		}
	}
}
func RefreshOutbounds() {
	outboundMu.Lock()
	defer outboundMu.Unlock()
	for tag, proc := range outboundProcesses {
		select {
		case <-proc.done:
		default:
			continue
		}
		if time.Now().Before(proc.nextRetry) {
			continue
		}
		stopOutbound(proc)
		if err := startOutbound(tag, proc); err != nil {
			logger.Warningf("managed VPN outbound %q: %v", tag, err)
		}
		// Do not run a tight restart loop for an invalid config or unreachable
		// runtime. Keep the same bridge address through every recovery attempt.
		proc.failures++
		if proc.failures > 6 {
			proc.failures = 6
		}
		proc.nextRetry = time.Now().Add(time.Duration(1<<proc.failures) * time.Second)
	}
}

func stopOutboundProtocol(protocol model.Protocol) {
	outboundMu.Lock()
	defer outboundMu.Unlock()
	for _, proc := range outboundProcesses {
		p, _ := proc.raw["protocol"].(string)
		if strings.EqualFold(p, string(protocol)) {
			stopOutbound(proc)
			proc.nextRetry = time.Time{}
		}
	}
}
