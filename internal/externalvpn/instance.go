package externalvpn

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

type Client struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Enable   bool   `json:"enable"`
}

type OpenFluxTransport struct {
	Type     string `json:"type"`
	URL      string `json:"url,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Dial     string `json:"dial,omitempty"`
}

type Settings struct {
	CertificatePEM string              `json:"certificatePEM"`
	PrivateKeyPEM  string              `json:"privateKeyPEM"`
	Image          string              `json:"image"`
	MTU            int                 `json:"mtu"`
	MaxSessions    int                 `json:"maxSessions"`
	Bandwidth      int                 `json:"bandwidth"`
	DetectProbing  bool                `json:"detectProbing"`
	AllowedSNI     string              `json:"allowedSni"`
	AdsFilter      bool                `json:"adsFilter"`
	TorrentFilter  bool                `json:"torrentFilter"`
	SpamFilter     bool                `json:"spamFilter"`
	MetricsKey     string              `json:"metricsKey"`
	Context        string              `json:"context"`
	Transports     []OpenFluxTransport `json:"transports"`

	Key            int      `json:"key"`
	Encrypt        string   `json:"encrypt"`
	EncryptKey     string   `json:"encryptKey"`
	MaxConn        int      `json:"maxConn"`
	ConnectTimeout int      `json:"connectTimeout"`
	Forward        string   `json:"forward"`
	Congestion     string   `json:"congestion"`
	Hostname       string   `json:"hostname"`
	Certificate    string   `json:"certificate"`
	PrivateKey     string   `json:"privateKey"`
	Clients        []Client `json:"clients"`
}

type Instance struct {
	ID       int
	Tag      string
	Protocol model.Protocol
	Listen   string
	Port     int
	Settings Settings
}

func FromInbound(ib *model.Inbound) (Instance, error) {
	if ib == nil || !IsManaged(ib.Protocol) {
		return Instance{}, fmt.Errorf("unsupported external VPN inbound")
	}
	var settings Settings
	if err := json.Unmarshal([]byte(ib.Settings), &settings); err != nil {
		return Instance{}, fmt.Errorf("%s settings: %w", ib.Protocol, err)
	}
	inst := Instance{ID: ib.Id, Tag: ib.Tag, Protocol: ib.Protocol, Listen: ib.Listen, Port: ib.Port, Settings: settings}
	return inst, inst.Validate()
}

func (inst Instance) Validate() error {
	s := inst.Settings
	if inst.Protocol == model.FPTN || inst.Protocol == model.OpenFlux {
		return inst.validateAdditional()
	}
	if inst.Protocol == model.Pingtunnel {
		if inst.Port != 0 {
			return fmt.Errorf("Pingtunnel uses ICMP and requires port 0")
		}
		if s.Key < 1 || s.Key > 2147483647 {
			return fmt.Errorf("Pingtunnel key must be 1..2147483647")
		}
		if s.Encrypt != "" && s.Encrypt != "chacha20" && s.Encrypt != "aes256" && s.Encrypt != "aes128" {
			return fmt.Errorf("Pingtunnel encryption must be empty, chacha20, aes256 or aes128")
		}
		if s.Encrypt != "" && s.EncryptKey == "" {
			return fmt.Errorf("Pingtunnel encryption key is required when encryption is enabled")
		}
		if s.MaxConn < 0 || s.ConnectTimeout < 0 {
			return fmt.Errorf("Pingtunnel connection limit and timeout must be nonnegative")
		}
		if s.Congestion != "" && s.Congestion != "bb" && s.Congestion != "none" {
			return fmt.Errorf("Pingtunnel congestion must be bb or none")
		}
		if s.Forward != "" {
			u, err := url.Parse(s.Forward)
			if err != nil || (u.Scheme != "socks5" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
				return fmt.Errorf("Pingtunnel forward must be a socks5://host:port or http://host:port URL")
			}
			port, err := strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				return fmt.Errorf("Pingtunnel forward must have a valid port")
			}
		}
		if inst.Listen != "" && net.ParseIP(inst.Listen) == nil {
			return fmt.Errorf("Pingtunnel listen must be an IP address")
		}
		return nil
	}
	if inst.Port < 1 || inst.Port > 65535 {
		return fmt.Errorf("TrustTunnel requires a TCP port")
	}
	if inst.Listen != "" && net.ParseIP(inst.Listen) == nil {
		return fmt.Errorf("TrustTunnel listen must be an IP address")
	}
	host := strings.TrimSpace(s.Hostname)
	if host == "" || strings.ContainsAny(host, " \t\r\n/:\\\"") {
		return fmt.Errorf("TrustTunnel hostname is required")
	}
	if (s.Certificate == "") != (s.PrivateKey == "") {
		return fmt.Errorf("TrustTunnel certificate and privateKey must both be provided")
	}
	seen := make(map[string]bool)
	for _, c := range s.Clients {
		if !c.Enable {
			continue
		}
		if c.Email == "" || strings.ContainsAny(c.Email, "\r\n\x00") || c.Password == "" || strings.ContainsAny(c.Password, "\r\n\x00") {
			return fmt.Errorf("TrustTunnel clients need an email and password without control characters")
		}
		if seen[c.Email] {
			return fmt.Errorf("duplicate TrustTunnel client %q", c.Email)
		}
		seen[c.Email] = true
	}
	return nil
}

func (inst Instance) Bind() string {
	addr := inst.Listen
	if addr == "" {
		addr = "0.0.0.0"
	}
	return net.JoinHostPort(addr, strconv.Itoa(inst.Port))
}

func GeneratePingtunnelKey() (int, error) {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return 0, err
	}
	return int((uint32(bytes[0])<<24|uint32(bytes[1])<<16|uint32(bytes[2])<<8|uint32(bytes[3]))%2147483647) + 1, nil
}

func GenerateSecret() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	const alphabet = "0123456789abcdef"
	var out [64]byte
	for i, b := range bytes {
		out[i*2], out[i*2+1] = alphabet[b>>4], alphabet[b&15]
	}
	return string(out[:]), nil
}

func IsManaged(p model.Protocol) bool {
	return p == model.Pingtunnel || p == model.TrustTunnel || p == model.FPTN || p == model.OpenFlux
}
