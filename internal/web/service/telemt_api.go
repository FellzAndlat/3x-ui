package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/SawaMEN/3x-ui/v3/internal/mtproto"
	"github.com/pelletier/go-toml/v2"
)

type telemtAPISettings struct {
	Listen string `toml:"listen"`
	Auth   string `toml:"auth_header"`
}

func readTelemtAPISettings() telemtAPISettings {
	var raw struct {
		Server struct {
			API telemtAPISettings `toml:"api"`
		} `toml:"server"`
	}
	if b, err := os.ReadFile(telemtConfigPath); err == nil {
		_ = toml.Unmarshal(b, &raw)
	}
	return raw.Server.API
}

func validTelemtAPIListen(listen string) bool {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	n, err := strconv.Atoi(port)
	return ip != nil && ip.IsLoopback() && err == nil && n > 0 && n <= 65535
}

func telemtAPISettingsForRender(proxyPort int) (telemtAPISettings, error) {
	return prepareTelemtAPISettings(readTelemtAPISettings(), proxyPort)
}

func prepareTelemtAPISettings(api telemtAPISettings, proxyPort int) (telemtAPISettings, error) {
	_, port, _ := net.SplitHostPort(api.Listen)
	if !validTelemtAPIListen(api.Listen) || port == strconv.Itoa(proxyPort) {
		free, err := mtproto.FreeLocalPort()
		if err != nil {
			return api, err
		}
		api.Listen = fmt.Sprintf("127.0.0.1:%d", free)
	}
	if api.Auth == "" {
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			return api, err
		}
		api.Auth = "Bearer " + hex.EncodeToString(token[:])
	}
	return api, nil
}

func telemtAPIRequest(method, path string) (*http.Request, error) {
	api := readTelemtAPISettings()
	if api.Listen == "" {
		api.Listen = "127.0.0.1:9091"
	} // existing standalone installs
	if !validTelemtAPIListen(api.Listen) {
		return nil, fmt.Errorf("telemt: control API must use a loopback listener")
	}
	req, err := http.NewRequestWithContext(context.Background(), method, "http://"+api.Listen+path, nil)
	if err == nil && api.Auth != "" {
		req.Header.Set("Authorization", api.Auth)
	}
	return req, err
}
