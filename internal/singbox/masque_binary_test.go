package singbox

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMASQUEBinaryConfig(t *testing.T) {
	binary := os.Getenv("SINGBOX_TEST_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_TEST_BINARY for native MASQUE config checks")
	}
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{1, 2, 3} {
		endpoint, err := TranslateMASQUEEndpoint(map[string]any{"protocol": "masque", "tag": "masque", "listen": "127.0.0.1", "port": 18443, "settings": map[string]any{"version": []int{version}, "tls": map[string]any{"certificatePath": certPath, "keyPath": keyPath}, "clients": []any{map[string]any{"email": "alice", "password": "secret", "enable": true}}}})
		if err != nil {
			t.Fatal(err)
		}
		configs := map[string]any{
			"server": map[string]any{"endpoints": []any{endpoint}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}, "route": map[string]any{"final": "direct"}},
			"client": map[string]any{"endpoints": []any{map[string]any{"type": "masque-client", "tag": "proxy", "server": "127.0.0.1", "server_port": 18443, "version": version, "username": "alice", "password": "secret", "tls": map[string]any{"enabled": true, "server_name": "localhost", "certificate_path": certPath}}}, "route": map[string]any{"final": "proxy"}},
		}
		for name, cfg := range configs {
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, name+".json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "check", "-c", path)
			cmd.Dir = filepath.Dir(binary)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("HTTP/%d %s check: %v: %s", version, name, err, output)
			}
		}
	}
}
