package youtubeproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrustedClientHTTPSFilteringEndToEnd(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Alt-Svc", `h3=":443"`)
		_, _ = io.WriteString(w, `{"adSlots":[1],"streamingData":{"url":"keep"}}`)
	}))
	defer upstream.Close()
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// Test-only upstream CA and dial mapping; production always validates public DNS/TLS.
	pool := x509.NewCertPool()
	pool.AddCert(upstream.Certificate())
	address := strings.TrimPrefix(upstream.URL, "https://")
	p.transport.TLSClientConfig = &tls.Config{RootCAs: pool, ServerName: upstream.Certificate().DNSNames[0], MinVersion: tls.VersionTLS12}
	p.transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	proxy := httptest.NewServer(p.handler(t.TempDir()))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	roots := x509.NewCertPool()
	roots.AddCert(p.ca.cert)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}, ForceAttemptHTTP2: false}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for i := 0; i < 2; i++ {
		response, err := client.Get("https://www.youtube.com/youtubei/v1/player")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if strings.Contains(string(body), "adSlots") || !strings.Contains(string(body), "keep") || response.Header.Get("Alt-Svc") != "" {
			t.Fatalf("bad forwarded response: %s", body)
		}
	}
	if p.filtered.Load() != 2 {
		t.Fatal("filtered response statistics missing")
	}
}
func TestCAIsPrivateStableAndRestrictsHosts(t *testing.T) {
	dir := t.TempDir()
	first, err := loadAuthority(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadAuthority(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.cert.Raw) != string(second.cert.Raw) {
		t.Fatal("CA rotated on startup")
	}
	public, _ := os.ReadFile(filepath.Join(dir, "ca-cert.pem"))
	block, rest := pem.Decode(public)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		t.Fatal("private key in public export")
	}
	if _, err := first.leaf("accounts.google.com"); err == nil {
		t.Fatal("interception escaped YouTube")
	}
	cert, err := first.leaf("www.youtube.com")
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(first.cert)
	if _, err = cert.Leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "www.youtube.com"}); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(filepath.Join(dir, "ca-private.pem"), 0644)
	if _, err = loadAuthority(dir); err == nil {
		t.Fatal("world-readable CA accepted")
	}
}
func TestProxyRejectsHostPivotAndPublicListener(t *testing.T) {
	if err := Run(context.Background(), "0.0.0.0:18080", t.TempDir()); err == nil {
		t.Fatal("public listener accepted")
	}
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	recorder := httptest.NewRecorder()
	p.forward(recorder, httptest.NewRequest("GET", "https://accounts.google.com/", nil), "www.youtube.com")
	if recorder.Code != 421 {
		t.Fatal("CONNECT host pivot accepted")
	}
	r := httptest.NewRequest("CONNECT", "http://127.0.0.1:22", nil)
	r.Host = "127.0.0.1:22"
	recorder = httptest.NewRecorder()
	p.ServeHTTP(recorder, r)
	if recorder.Code != 400 {
		t.Fatal("non-HTTPS target accepted")
	}
}
func TestOtherHostsTunnelWithoutInterception(t *testing.T) {
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() { defer server.Close(); _, _ = io.Copy(server, server) }()
		return client, nil
	}
	proxy := httptest.NewServer(p.handler(t.TempDir()))
	defer proxy.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = fmt.Fprint(conn, "CONNECT googlevideo.com:443 HTTP/1.1\r\nHost: googlevideo.com:443\r\n\r\n")
	reader := bufio.NewReader(conn)
	r, err := http.ReadResponse(reader, nil)
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("CONNECT failed %v", err)
	}
	_, _ = fmt.Fprint(conn, "opaque TLS bytes")
	buffer := make([]byte, len("opaque TLS bytes"))
	if _, err = io.ReadFull(reader, buffer); err != nil || string(buffer) != "opaque TLS bytes" {
		t.Fatalf("tunnel modified: %s %v", buffer, err)
	}
}

func TestRunStopsAndExportsOnlyPublicCA(t *testing.T) {
	dir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, address, dir) }()
	client := &http.Client{Timeout: time.Second}
	var response *http.Response
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err = client.Get("http://" + address + "/ca-cert.pem")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || strings.Contains(string(body), "PRIVATE KEY") {
		t.Fatal("bad public export")
	}
	response, err = client.Get("http://" + address + "/status")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("status unavailable")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("proxy did not stop")
	}
}

func TestUpstreamTLSIsVerified(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted upstream reached handler") }))
	defer upstream.Close()
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	address := strings.TrimPrefix(upstream.URL, "https://")
	p.transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	proxy := httptest.NewServer(p)
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	roots := x509.NewCertPool()
	roots.AddCert(p.ca.cert)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Get("https://www.youtube.com/youtubei/v1/player")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatal("untrusted upstream accepted")
	}
}
