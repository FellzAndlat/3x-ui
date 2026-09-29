package externalvpn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

type running struct {
	protocol    model.Protocol
	metricsAddr string
	tag         string
	counters    map[string]trafficCounters
	fingerprint string
	cancel      context.CancelFunc
	cmd         *exec.Cmd
	done        chan struct{}
}

type Manager struct {
	mu         sync.Mutex
	procs      map[int]*running
	lastErrors map[int]string
}

var global = &Manager{procs: map[int]*running{}, lastErrors: map[int]string{}}

func GetManager() *Manager { return global }

func directory() string {
	path, err := filepath.Abs(filepath.Join(config.GetBinFolderPath(), "externalvpn"))
	if err != nil {
		return filepath.Join(config.GetBinFolderPath(), "externalvpn")
	}
	return path
}

func binary(protocol model.Protocol) string {
	name := string(protocol)
	if protocol == model.TrustTunnel {
		name = "trusttunnel_endpoint"
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, path := range []string{filepath.Join(config.GetBinFolderPath(), name), "/usr/local/bin/" + name, "/usr/bin/" + name} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	return filepath.Join(config.GetBinFolderPath(), name)
}

func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(0600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func tomlString(s string) string { return strconv.Quote(s) }

func certificate(inst Instance, folder string) (string, string, error) {
	if inst.Settings.Certificate != "" {
		pair, err := tls.LoadX509KeyPair(inst.Settings.Certificate, inst.Settings.PrivateKey)
		if err != nil {
			return "", "", err
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return "", "", err
		}
		if err := leaf.VerifyHostname(inst.Settings.Hostname); err != nil {
			return "", "", err
		}
		return inst.Settings.Certificate, inst.Settings.PrivateKey, nil
	}
	certPath, keyPath := filepath.Join(folder, "cert.pem"), filepath.Join(folder, "key.pem")
	if data, err := os.ReadFile(certPath); err == nil {
		if block, _ := pem.Decode(data); block != nil {
			if cert, err := x509.ParseCertificate(block.Bytes); err == nil && cert.VerifyHostname(inst.Settings.Hostname) == nil && time.Until(cert.NotAfter) > 30*24*time.Hour {
				if _, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
					return certPath, keyPath, nil
				}
			}
		}
	}
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: inst.Settings.Hostname}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	if ip := net.ParseIP(inst.Settings.Hostname); ip != nil {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	} else {
		tmpl.DNSNames = []string{inst.Settings.Hostname}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	if err := writePrivate(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})); err != nil {
		return "", "", err
	}
	if err := writePrivate(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}

func files(inst Instance, metricsAddr string) ([]string, error) {
	folder := filepath.Join(directory(), strconv.Itoa(inst.ID))
	if err := os.MkdirAll(folder, 0700); err != nil {
		return nil, err
	}
	if inst.Protocol == model.Pingtunnel {
		path := filepath.Join(folder, "server.json")
		data, err := json.Marshal(map[string]any{"type": "server", "key": inst.Settings.Key, "encrypt": inst.Settings.Encrypt, "encrypt_key": inst.Settings.EncryptKey, "icmp_listen": defaultListen(inst.Listen), "maxconn": inst.Settings.MaxConn, "conntt": inst.Settings.ConnectTimeout, "forward": inst.Settings.Forward, "nolog": 1})
		if err != nil {
			return nil, err
		}
		if err := writePrivate(path, data); err != nil {
			return nil, err
		}
		args := []string{"-c", path}
		if inst.Settings.Congestion == "none" {
			// Pingtunnel's JSON loader ignores empty strings; disable via an explicit flag.
			args = append(args, "-congestion", "")
		}
		return args, nil
	}
	cert, key, err := certificate(inst, folder)
	if err != nil {
		return nil, err
	}
	credentials := filepath.Join(folder, "credentials.toml")
	var users strings.Builder
	for _, c := range inst.Settings.Clients {
		if c.Enable {
			fmt.Fprintf(&users, "[[client]]\nusername = %s\npassword = %s\n", tomlString(c.Email), tomlString(c.Password))
		}
	}
	if err := writePrivate(credentials, []byte(users.String())); err != nil {
		return nil, err
	}
	vpn := filepath.Join(folder, "vpn.toml")
	settings := fmt.Sprintf("listen_address = %s\ncredentials_file = %s\nallow_private_network_connections = false\n[listen_protocols.http2]\n[listen_protocols.quic]\n[forward_protocol]\ndirect = {}\n[metrics]\naddress = %s\nper_client_metrics = true\n", tomlString(inst.Bind()), tomlString(credentials), tomlString(metricsAddr))
	if err := writePrivate(vpn, []byte(settings)); err != nil {
		return nil, err
	}
	hosts := filepath.Join(folder, "hosts.toml")
	tls := fmt.Sprintf("[[main_hosts]]\nhostname = %s\ncert_chain_path = %s\nprivate_key_path = %s\n", tomlString(inst.Settings.Hostname), tomlString(cert), tomlString(key))
	if err := writePrivate(hosts, []byte(tls)); err != nil {
		return nil, err
	}
	return []string{vpn, hosts}, nil
}

func defaultListen(listen string) string {
	if listen == "" {
		return "0.0.0.0"
	}
	return listen
}

func fingerprint(inst Instance) string {
	data, _ := json.Marshal(inst)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func certificateRenewalNeeded(inst Instance) bool {
	if inst.Protocol != model.TrustTunnel || inst.Settings.Certificate != "" {
		return false
	}
	path := filepath.Join(directory(), strconv.Itoa(inst.ID), "cert.pem")
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return true
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	return err != nil || time.Until(cert.NotAfter) < 30*24*time.Hour
}

func (m *Manager) Ensure(inst Instance) error {
	if err := inst.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureLocked(inst)
}

func (m *Manager) ensureLocked(inst Instance) error {
	fp := fingerprint(inst)
	if old := m.procs[inst.ID]; old != nil {
		select {
		case <-old.done:
			delete(m.procs, inst.ID)
		default:
			if old.fingerprint == fp && !certificateRenewalNeeded(inst) {
				return nil
			}
			m.removeLocked(inst.ID)
		}
	}
	if inst.Protocol == model.TrustTunnel {
		active := false
		for _, c := range inst.Settings.Clients {
			active = active || c.Enable
		}
		if !active {
			return nil
		}
	}
	if inst.Protocol == model.Pingtunnel {
		for id, proc := range m.procs {
			if id != inst.ID && proc.protocol == model.Pingtunnel {
				return fmt.Errorf("only one Pingtunnel listener may run per server")
			}
		}
	}
	metricsAddr := ""
	if inst.Protocol == model.TrustTunnel {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		metricsAddr = listener.Addr().String()
		_ = listener.Close()
	}
	args, err := files(inst, metricsAddr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary(inst.Protocol), args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("%s: %w", inst.Protocol, err)
	}
	proc := &running{protocol: inst.Protocol, fingerprint: fp, cancel: cancel, cmd: cmd, done: make(chan struct{}), metricsAddr: metricsAddr, tag: inst.Tag, counters: map[string]trafficCounters{}}
	m.procs[inst.ID] = proc
	go func() {
		err := cmd.Wait()
		close(proc.done)
		if ctx.Err() == nil {
			logger.Warningf("%s inbound %d exited: %v", inst.Protocol, inst.ID, err)
		}
	}()
	return nil
}

func (m *Manager) removeLocked(id int) {
	if proc := m.procs[id]; proc != nil {
		proc.cancel()
		select {
		case <-proc.done:
		case <-time.After(2 * time.Second):
			_ = proc.cmd.Process.Kill()
			<-proc.done
		}
		delete(m.procs, id)
	}
}
func (m *Manager) Remove(id int) { m.mu.Lock(); defer m.mu.Unlock(); m.removeLocked(id) }
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.procs {
		m.removeLocked(id)
	}
}

func (m *Manager) Reconcile(instances []Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sort.Slice(instances, func(i, j int) bool { return instances[i].ID < instances[j].ID })
	desired := map[int]bool{}
	for _, inst := range instances {
		desired[inst.ID] = true
	}
	for id := range m.procs {
		if !desired[id] {
			m.removeLocked(id)
		}
	}
	for _, inst := range instances {
		if err := m.ensureLocked(inst); err != nil {
			if m.lastErrors[inst.ID] != err.Error() {
				logger.Warningf("%s inbound %d: %v", inst.Protocol, inst.ID, err)
				m.lastErrors[inst.ID] = err.Error()
			}
		} else {
			delete(m.lastErrors, inst.ID)
		}
	}
}

func ExportTrustTunnelLink(inst Instance, email, address string, port int) (string, error) {
	if inst.Protocol != model.TrustTunnel {
		return "", fmt.Errorf("not a TrustTunnel inbound")
	}
	address = strings.Trim(strings.TrimSpace(address), "[]")
	if address == "" || net.ParseIP(address) != nil && net.ParseIP(address).IsUnspecified() {
		return "", fmt.Errorf("a public address is required")
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("a valid port is required")
	}
	found := false
	for _, c := range inst.Settings.Clients {
		if c.Email == email && c.Enable {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("client is disabled or absent")
	}
	folder := filepath.Join(directory(), strconv.Itoa(inst.ID))
	vpn, hosts := filepath.Join(folder, "vpn.toml"), filepath.Join(folder, "hosts.toml")
	if _, err := os.Stat(vpn); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := net.JoinHostPort(address, strconv.Itoa(port))
	output, err := exec.CommandContext(ctx, binary(model.TrustTunnel), vpn, hosts, "-c", email, "-a", target, "--format", "deeplink").Output()
	if err != nil {
		return "", err
	}
	link := strings.TrimSpace(string(output))
	if !strings.HasPrefix(link, "tt://?") {
		return "", fmt.Errorf("TrustTunnel returned an invalid deep link")
	}
	return link, nil
}

type trafficCounters struct{ up, down uint64 }
type TrafficDelta struct {
	Tag, Email string
	Up, Down   int64
	Active     bool
}

func (m *Manager) CollectTraffic() []TrafficDelta {
	m.mu.Lock()
	defer m.mu.Unlock()
	var deltas []TrafficDelta
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	for _, proc := range m.procs {
		if proc.metricsAddr == "" {
			continue
		}
		response, err := client.Get("http://" + proc.metricsAddr + "/clients")
		if err != nil {
			continue
		}
		var clients []struct {
			Username string `json:"username"`
			Sessions int    `json:"sessions"`
			Inbound  uint64 `json:"inbound"`
			Outbound uint64 `json:"outbound"`
		}
		if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&clients) != nil {
			response.Body.Close()
			continue
		}
		response.Body.Close()
		for _, row := range clients {
			if row.Username == "" {
				continue
			}
			old := proc.counters[row.Username]
			up, down := row.Inbound, row.Outbound
			delta := TrafficDelta{
				Tag:    proc.tag,
				Email:  row.Username,
				Up:     cumulativeTrafficDelta(up, old.up),
				Down:   cumulativeTrafficDelta(down, old.down),
				Active: row.Sessions > 0,
			}
			if delta.Up > 0 || delta.Down > 0 || delta.Active {
				deltas = append(deltas, delta)
			}
			proc.counters[row.Username] = trafficCounters{up: up, down: down}
		}
	}
	return deltas
}
