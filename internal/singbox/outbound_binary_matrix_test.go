package singbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Check the emitted native options against the installed core's own schema.
// Set SINGBOX_TEST_BINARY to a release binary to enable this contract test.
func TestNativeOutboundBinaryMatrix(t *testing.T) {
	binary := os.Getenv("SINGBOX_TEST_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_TEST_BINARY for native config checks")
	}
	cases := map[string]string{
		"direct":      `{}`,
		"block":       `{}`,
		"bridge":      `{"interface":"eth0"}`,
		"socks":       `{"server":"127.0.0.1","server_port":1080,"version":"5"}`,
		"http":        `{"server":"127.0.0.1","server_port":8080}`,
		"shadowsocks": `{"server":"127.0.0.1","server_port":443,"method":"aes-128-gcm","password":"secret"}`,
		"vmess":       `{"server":"127.0.0.1","server_port":443,"uuid":"2a547fe4-5b0f-4a84-845c-131464875bf8","security":"auto"}`,
		"vless":       `{"server":"127.0.0.1","server_port":443,"uuid":"2a547fe4-5b0f-4a84-845c-131464875bf8","packet_encoding":"xudp"}`,
		"trojan":      `{"server":"127.0.0.1","server_port":443,"password":"secret","tls":{"enabled":true}}`,
		"hysteria":    `{"server":"127.0.0.1","server_port":443,"up_mbps":100,"down_mbps":100,"tls":{"enabled":true}}`,
		"hysteria2":   `{"server":"127.0.0.1","server_port":443,"password":"secret","tls":{"enabled":true}}`,
		"tuic":        `{"server":"127.0.0.1","server_port":443,"uuid":"2a547fe4-5b0f-4a84-845c-131464875bf8","password":"secret","tls":{"enabled":true}}`,
		"shadowtls":   `{"server":"127.0.0.1","server_port":443,"version":3,"password":"secret","tls":{"enabled":true,"server_name":"example.com"}}`,
		"anytls":      `{"server":"127.0.0.1","server_port":443,"password":"secret","tls":{"enabled":true}}`,
		"snell":       `{"server":"127.0.0.1","server_port":443,"version":4,"psk":"secret"}`,
		"ssh":         `{"server":"127.0.0.1"}`,
		"tor":         `{}`,
		"naive":       `{"server":"127.0.0.1","server_port":443,"username":"proxy","password":"secret","tls":{"enabled":true}}`,
		"selector":    `{"outbounds":["direct"],"default":"direct"}`,
		"urltest":     `{"outbounds":["direct"],"interval":"3m"}`,
	}
	for protocol, raw := range cases {
		t.Run(protocol, func(t *testing.T) {
			var settings map[string]any
			if err := json.Unmarshal([]byte(raw), &settings); err != nil {
				t.Fatal(err)
			}
			outbound, err := TranslateXrayOutbound(map[string]any{"protocol": "singbox:" + protocol, "tag": "proxy", "settings": settings})
			if err != nil {
				t.Fatal(err)
			}
			cfg := NewConfig()
			cfg.Log = map[string]any{"level": "warn"}
			cfg.Outbounds = append(cfg.Outbounds, outbound)
			data, err := cfg.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(binary, "check", "-c", path)
			command.Dir = filepath.Dir(binary)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("sing-box check: %v: %s", err, output)
			}
		})
	}
}
