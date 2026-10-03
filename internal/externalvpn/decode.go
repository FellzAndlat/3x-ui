package externalvpn

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
)

type FPTNServer struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Fingerprint string `json:"md5_fingerprint"`
}
type FPTNToken struct {
	Version  int          `json:"version"`
	Name     string       `json:"service_name"`
	Username string       `json:"username"`
	Password string       `json:"password"`
	Servers  []FPTNServer `json:"servers"`
	Censored []FPTNServer `json:"censored_zone_servers"`
}

func DecodeFPTNToken(token string) (FPTNToken, error) {
	var result FPTNToken
	if !strings.HasPrefix(token, "fptn:") || len(token) > 64<<10 {
		return result, fmt.Errorf("invalid FPTN token")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(strings.TrimPrefix(token, "fptn:"), "="))
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	if result.Version != 1 || result.Username == "" || result.Password == "" || len(result.Servers)+len(result.Censored) == 0 {
		return result, fmt.Errorf("incomplete FPTN token")
	}
	for _, s := range append(append([]FPTNServer{}, result.Servers...), result.Censored...) {
		if _, err := hex.DecodeString(s.Fingerprint); err != nil {
			return result, fmt.Errorf("invalid FPTN certificate fingerprint")
		}
		if s.Host == "" || strings.ContainsAny(s.Host, "\r\n\x00 /\\") || s.Port < 1 || s.Port > 65535 || len(s.Fingerprint) != 32 {
			return result, fmt.Errorf("invalid FPTN server endpoint")
		}
		if ip := net.ParseIP(s.Host); ip != nil && ip.IsUnspecified() {
			return result, fmt.Errorf("FPTN endpoint is unspecified")
		}
	}
	return result, nil
}

type OpenFluxLink struct {
	Name       string              `json:"name"`
	Negotiate  bool                `json:"negotiate"`
	Codec      string              `json:"codec"`
	Secret     string              `json:"secret"`
	Context    string              `json:"context"`
	Mode       string              `json:"mode"`
	Transports []OpenFluxTransport `json:"transports"`
}

func DecodeOpenFluxLink(link string) (OpenFluxLink, error) {
	var result OpenFluxLink
	if !strings.HasPrefix(link, "openflux://v1/") || len(link) > 64<<10 {
		return result, fmt.Errorf("unsupported OpenFlux link")
	}
	packed, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimPrefix(link, "openflux://v1/"), "="))
	if err != nil {
		return result, err
	}
	reader := flate.NewReader(bytes.NewReader(packed))
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, (16<<10)+1))
	if err != nil {
		return result, err
	}
	if len(raw) > 16<<10 {
		return result, fmt.Errorf("OpenFlux link too large")
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	if !result.Negotiate || result.Mode != "" || (result.Codec != "" && result.Codec != "batched") {
		return result, fmt.Errorf("panel requires negotiated, encrypted batched OpenFlux sessions")
	}
	return result, nil
}
