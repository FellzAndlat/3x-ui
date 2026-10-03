// Package snell contains the panel's Snell credential and version contract.
package snell

import (
 "crypto/rand"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "strings"

 "github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

type Settings struct {
 Version int `json:"version"`
 PSK string `json:"psk"`
 Mode string `json:"mode"`
 ObfsMode string `json:"obfsMode"`
 ObfsHost string `json:"obfsHost"`
 Clients []model.Client `json:"clients"`
}

func Secret() (string, error) {
 var b [32]byte
 if _, err := rand.Read(b[:]); err != nil { return "", err }
 return hex.EncodeToString(b[:]), nil
}

func Parse(raw string) (Settings, error) {
 var s Settings
 if err := json.Unmarshal([]byte(raw), &s); err != nil { return s, err }
 if s.Version != 5 && s.Version != 6 { return s, fmt.Errorf("Snell server version must be 5 or 6") }
 if s.PSK == "" || strings.ContainsAny(s.PSK, "\r\n\x00") { return s, fmt.Errorf("Snell requires a PSK without control characters") }
 if s.Version == 6 {
  if len(s.PSK) < 12 || len(s.PSK) > 255 { return s, fmt.Errorf("Snell v6 PSK must be 12..255 bytes") }
  if s.Mode != "" && s.Mode != "default" && s.Mode != "unshaped" { return s, fmt.Errorf("Snell v6 mode must be default or unshaped") }
  if s.ObfsMode != "" && s.ObfsMode != "none" { return s, fmt.Errorf("Snell v6 uses mode instead of obfsMode") }
 } else {
  if s.ObfsMode != "" && s.ObfsMode != "none" && s.ObfsMode != "http" { return s, fmt.Errorf("Snell v5 obfsMode must be none or http") }
  if s.Mode != "" && s.Mode != "default" { return s, fmt.Errorf("Snell v5 does not support traffic shaping modes") }
 }
 seen := map[string]bool{}
 for _, c := range s.Clients {
  if !c.Enable { continue }
  if c.Email == "" || c.Password == "" { return s, fmt.Errorf("Snell clients require a name and user key") }
  if seen[c.Password] { return s, fmt.Errorf("Snell user keys must be unique") }
  seen[c.Password] = true
 }
 return s, nil
}

// Prepare seeds credentials only when missing, keeping existing subscriptions valid.
func Prepare(ib *model.Inbound, previous string) error {
 if ib.Protocol != model.Snell { return nil }
 var raw map[string]any
 if err := json.Unmarshal([]byte(ib.Settings), &raw); err != nil { return err }
 if raw == nil { raw = map[string]any{} }
 var old Settings
 _ = json.Unmarshal([]byte(previous), &old)
 if raw["version"] == nil { raw["version"] = 6 }
 if raw["psk"] == nil || raw["psk"] == "" {
  secret := old.PSK
  if secret == "" { var err error; secret, err = Secret(); if err != nil { return err } }
  raw["psk"] = secret
 }
 previousKeys := map[string]string{}
 for _, c := range old.Clients { previousKeys[c.Email] = c.Password }
 if clients, ok := raw["clients"].([]any); ok {
  for _, entry := range clients {
   c, ok := entry.(map[string]any); if !ok { return fmt.Errorf("invalid Snell client") }
   if c["password"] == nil || c["password"] == "" {
    email, _ := c["email"].(string)
    secret := previousKeys[email]
    if secret == "" { var err error; secret, err = Secret(); if err != nil { return err } }
    c["password"] = secret
   }
  }
 }
 data, err := json.Marshal(raw); if err != nil { return err }
 ib.Settings = string(data)
 _, err = Parse(ib.Settings)
 return err
}

// sing-box deliberately calls the v5-compatible outbound version 4.
func (s Settings) ClientVersion() int { if s.Version == 5 { return 4 }; return 6 }

func (s Settings) Outbound(address string, port int, client model.Client) map[string]any {
 out := map[string]any{"type":"snell", "server":address, "server_port":port, "version":s.ClientVersion(), "psk":s.PSK, "userkey":client.Password}
 if s.Version == 6 { if s.Mode != "" { out["mode"] = s.Mode } } else {
  if s.ObfsMode != "" { out["obfs_mode"] = s.ObfsMode }
  if s.ObfsMode == "http" && s.ObfsHost != "" { out["obfs_host"] = s.ObfsHost }
 }
 return out
}
