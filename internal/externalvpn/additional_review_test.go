package externalvpn

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestManagedVPNOpenFluxContract(t *testing.T) {
	t.Setenv("XUI_BIN_FOLDER", t.TempDir())
	secret := strings.Repeat("a", 64)
	inst := Instance{ID: 1, Protocol: model.OpenFlux, Port: 443, Settings: Settings{Context: "custom encryption context", Clients: []Client{{Email: "user", Enable: true, Password: secret}}, Transports: []OpenFluxTransport{{Type: "direct", Priority: 100}}}}
	cmd, cancel, err := GetManager().additionalCommand(inst, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if cmd.Args[len(cmd.Args)-1] != "--negotiate" {
		t.Fatal("legacy fallback remains enabled")
	}
	conf, err := os.ReadFile(filepath.Join(directory(), "1", "openflux.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(conf), "SessionContext = custom encryption context\n") {
		t.Fatal("KDF context was not preserved")
	}
	if !strings.Contains(string(conf), "CookieStore = "+filepath.Join(directory(), "1", "cookies.json")) {
		t.Fatal("cookies are not isolated")
	}
	exported, err := ExportAdditionalLink(inst, "user", "2001:db8::1", 8443, "profile")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeOpenFluxLink(exported)
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string]any{"protocol": "openflux", "settings": map[string]any{"secret": decoded.Secret, "context": decoded.Context, "transports": decoded.Transports}}
	if err := ValidateAdditionalOutbound(raw); err != nil {
		t.Fatal(err)
	}
	if decoded.Transports[0].Dial != "[2001:db8::1]:8443" || decoded.Context != inst.Settings.Context {
		t.Fatal("round trip lost endpoint or context")
	}
	decoded.Transports[0].Dial = "127.0.0.1\nRole=exit:443"
	raw["settings"].(map[string]any)["transports"] = decoded.Transports
	if err := ValidateAdditionalOutbound(raw); err == nil {
		t.Fatal("INI injection accepted")
	}
}

func TestManagedVPNFPTNCertificate(t *testing.T) {
	ib := &model.Inbound{Protocol: model.FPTN, Port: 443, Settings: `{"hostname":"192.0.2.1","clients":[{"email":"user","enable":true}]}`}
	if err := PrepareAdditional(ib, ""); err != nil {
		t.Fatal(err)
	}
	inst, err := FromInbound(ib)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fptnCertificateFiles(inst, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	link, err := ExportAdditionalLink(inst, "user", "192.0.2.1", 443, "profile")
	if err != nil {
		t.Fatal(err)
	}
	token, err := DecodeFPTNToken(link)
	if err != nil {
		t.Fatal(err)
	}
	if token.Username != FPTNUsername("user") || token.Password != inst.Settings.Clients[0].Password {
		t.Fatal("round trip lost credentials")
	}
	previous := ib.Settings
	if err := PrepareAdditional(ib, previous); err != nil {
		t.Fatal(err)
	}
	var unchanged Settings
	if err := json.Unmarshal([]byte(ib.Settings), &unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged.CertificatePEM != inst.Settings.CertificatePEM {
		t.Fatal("ordinary edit rotated certificate")
	}
}

func TestFPTNSessionCounterReset(t *testing.T) {
	cert, key, err := newFPTNCertificate("localhost")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair([]byte(cert), []byte(key))
	if err != nil {
		t.Fatal(err)
	}
	user := FPTNUsername("user")
	var metrics atomic.Value
	metrics.Store(fmt.Sprintf("fptn_user_incoming_traffic_bytes{session_id=\"1\",username=\"%s\"} 100\nfptn_user_incoming_traffic_bytes{username=\"%s\",session_id=\"2\"} 200\n", user, user))
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, metrics.Load().(string)) }))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	server.StartTLS()
	defer server.Close()
	proc := &running{tag: "vpn", protocol: model.FPTN, metricsAddr: strings.TrimPrefix(server.URL, "https://"), counters: map[string]trafficCounters{}, instance: Instance{Settings: Settings{Hostname: "localhost", CertificatePEM: cert, MetricsKey: "key", Clients: []Client{{Email: "user", Enable: true}}}}}
	rows := GetManager().collectAdditionalTraffic(proc)
	if len(rows) != 1 || rows[0].Down != 300 {
		t.Fatalf("first poll: %+v", rows)
	}
	metrics.Store(fmt.Sprintf("fptn_user_incoming_traffic_bytes{username=\"%s\",session_id=\"1\"} 10\nfptn_user_incoming_traffic_bytes{session_id=\"2\",username=\"%s\"} 250\n", user, user))
	rows = GetManager().collectAdditionalTraffic(proc)
	if len(rows) != 1 || rows[0].Down != 60 {
		t.Fatalf("independent reset lost bytes: %+v", rows)
	}
	if host, _, err := net.SplitHostPort(proc.metricsAddr); err != nil || host == "" {
		t.Fatal("invalid metrics endpoint")
	}
}
