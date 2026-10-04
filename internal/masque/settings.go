// Package masque prepares native sing-box CONNECT-IP servers.
package masque

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
)

const DefaultPath = "/.well-known/masque/ip/{target}/{ipproto}/"

type TLS struct {
	Enabled         bool   `json:"enabled"`
	ServerName      string `json:"serverName"`
	CertificatePath string `json:"certificatePath"`
	KeyPath         string `json:"keyPath"`
}
type Settings struct {
	Version         []int          `json:"version"`
	Path            string         `json:"path"`
	Address         []string       `json:"address"`
	AdvertiseRoutes []string       `json:"advertiseRoutes"`
	MTU             int            `json:"mtu"`
	TLS             TLS            `json:"tls"`
	Clients         []model.Client `json:"clients"`
}

func Parse(data string) (Settings, error) {
	var s Settings
	if strings.TrimSpace(data) != "" {
		if err := json.Unmarshal([]byte(data), &s); err != nil {
			return s, err
		}
	}
	if len(s.Version) == 0 {
		s.Version = []int{3, 2, 1}
	}
	seen := map[int]bool{}
	for _, v := range s.Version {
		if v < 1 || v > 3 || seen[v] {
			return s, fmt.Errorf("MASQUE HTTP versions must be unique values 1, 2 or 3")
		}
		seen[v] = true
	}
	if s.Path == "" {
		s.Path = DefaultPath
	}
	if !strings.HasPrefix(s.Path, "/") || strings.ContainsAny(s.Path, "\r\n?#") {
		return s, fmt.Errorf("MASQUE path must be an absolute URI template path")
	}
	if len(s.Address) == 0 {
		s.Address = []string{"172.31.255.1/24", "fd7a:115c:a1e0::1/64"}
	}
	families := map[bool]bool{}
	for _, addr := range s.Address {
		prefix, err := netip.ParsePrefix(addr)
		if err != nil || prefix.Addr().Is4In6() {
			return s, fmt.Errorf("invalid MASQUE tunnel prefix %q", addr)
		}
		is4 := prefix.Addr().Is4()
		if families[is4] || prefix.Bits() >= prefix.Addr().BitLen()-1 {
			return s, fmt.Errorf("MASQUE requires at most one usable tunnel prefix per IP family")
		}
		families[is4] = true
	}
	for _, route := range s.AdvertiseRoutes {
		if _, err := netip.ParsePrefix(route); err != nil {
			return s, fmt.Errorf("invalid MASQUE advertised route %q", route)
		}
	}
	if s.MTU == 0 {
		s.MTU = 1280
	}
	if s.MTU < 1280 || s.MTU > 65535 {
		return s, fmt.Errorf("MASQUE MTU must be 1280..65535")
	}
	s.TLS.Enabled = true
	if (strings.TrimSpace(s.TLS.CertificatePath) == "") != (strings.TrimSpace(s.TLS.KeyPath) == "") {
		return s, fmt.Errorf("MASQUE requires both TLS certificate and key, or neither")
	}
	return s, nil
}

func Prepare(ib *model.Inbound, previous string) error {
	if ib == nil || ib.Protocol != model.MASQUE {
		return nil
	}
	s, err := Parse(ib.Settings)
	if err != nil {
		return err
	}
	var old Settings
	_ = json.Unmarshal([]byte(previous), &old)
	passwords := map[string]string{}
	for _, c := range old.Clients {
		passwords[c.Email] = c.Password
	}
	seen := map[string]bool{}
	for i := range s.Clients {
		c := &s.Clients[i]
		if c.Email == "" || seen[c.Email] {
			return fmt.Errorf("MASQUE clients need unique nonempty usernames")
		}
		seen[c.Email] = true
		if c.Password == "" {
			c.Password = passwords[c.Email]
		}
		if c.Password == "" {
			c.Password, err = externalvpn.GenerateSecret()
			if err != nil {
				return err
			}
		}
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	ib.Settings = string(data)
	ib.StreamSettings, ib.Sniffing = "", ""
	return nil
}
