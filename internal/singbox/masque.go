package singbox

import (
	"encoding/json"
	"fmt"
	"github.com/SawaMEN/3x-ui/v3/internal/masque"
)

// TranslateMASQUEEndpoint emits an endpoint, never an inbound.
func TranslateMASQUEEndpoint(raw map[string]any) (map[string]any, error) {
	data, err := json.Marshal(rawObject(raw, "settings"))
	if err != nil {
		return nil, err
	}
	s, err := masque.Parse(string(data))
	if err != nil {
		return nil, err
	}
	if s.TLS.CertificatePath == "" || s.TLS.KeyPath == "" {
		return nil, fmt.Errorf("MASQUE requires a TLS certificate and private key")
	}
	port := rawInt(raw, "port")
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("MASQUE requires a valid listen port")
	}
	users := make([]map[string]any, 0, len(s.Clients))
	for _, c := range s.Clients {
		if !c.Enable {
			continue
		}
		if c.Email == "" || c.Password == "" {
			return nil, fmt.Errorf("MASQUE active client has no username or password")
		}
		users = append(users, map[string]any{"username": c.Email, "password": c.Password})
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("MASQUE requires at least one active user")
	}
	listen := rawString(raw, "listen")
	if listen == "" {
		listen = "0.0.0.0"
	}
	tls := map[string]any{"enabled": true, "certificate_path": s.TLS.CertificatePath, "key_path": s.TLS.KeyPath}
	if s.TLS.ServerName != "" {
		tls["server_name"] = s.TLS.ServerName
	}
	out := map[string]any{"type": "masque-server", "tag": rawString(raw, "tag"), "listen": listen, "listen_port": port, "version": s.Version, "path": s.Path, "address": s.Address, "mtu": s.MTU, "system": false, "users": users, "tls": tls}
	if len(s.AdvertiseRoutes) > 0 {
		out["advertise_routes"] = s.AdvertiseRoutes
	}
	return out, nil
}
