package youtubeproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/util/netsafe"
	"github.com/SawaMEN/3x-ui/v3/internal/web/network"
)

type Proxy struct {
	options          Options
	lastError        atomic.Value
	upstreamFailures atomic.Uint64
	unchanged        atomic.Uint64
	malformed        atomic.Uint64
	oversized        atomic.Uint64
	unsupported      atomic.Uint64
	ca               *authority
	transport        *http.Transport
	dial             func(context.Context, string, string) (net.Conn, error)
	permits          chan struct{}
	filterSlots      chan struct{}
	skippedBusy      atomic.Uint64
	mu               sync.Mutex
	connections      map[net.Conn]bool
	closing          bool
	filtered         atomic.Uint64
	requests         atomic.Uint64
	tlsFailures      atomic.Uint64
}

func New(caDir string) (*Proxy, error) {
	ca, err := loadAuthority(caDir)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = netsafe.SSRFGuardedDialContext
	transport.ForceAttemptHTTP2 = true
	transport.TLSHandshakeTimeout = 15 * time.Second
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.MaxIdleConns = 64
	transport.MaxIdleConnsPerHost = 16
	return &Proxy{options: DefaultOptions(), ca: ca, transport: transport, dial: netsafe.SSRFGuardedDialContext, permits: make(chan struct{}, 128), filterSlots: make(chan struct{}, 2), connections: map[net.Conn]bool{}}, nil
}
func (p *Proxy) Close() {
	p.mu.Lock()
	p.closing = true
	for conn := range p.connections {
		_ = conn.Close()
	}
	p.mu.Unlock()
	p.transport.CloseIdleConnections()
}
func (p *Proxy) track(conn net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		return false
	}
	p.connections[conn] = true
	return true
}
func (p *Proxy) untrack(conn net.Conn) { p.mu.Lock(); delete(p.connections, conn); p.mu.Unlock() }
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		http.Error(w, "CONNECT proxy: configure your HTTPS proxy client", http.StatusMethodNotAllowed)
		return
	}
	host, port, err := net.SplitHostPort(r.Host)
	host = strings.ToLower(host)
	if err != nil || port != "443" || host == "" || strings.ContainsAny(host, "/%@\\\x00") {
		http.Error(w, "only HTTPS port 443 is allowed", http.StatusBadRequest)
		return
	}
	select {
	case p.permits <- struct{}{}:
	default:
		http.Error(w, "filter capacity reached", http.StatusServiceUnavailable)
		return
	}
	defer func() { <-p.permits }()
	var upstream net.Conn
	if !interceptedHost(host) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		upstream, err = p.dial(ctx, "tcp", net.JoinHostPort(host, port))
		cancel()
		if err != nil {
			http.Error(w, "upstream connection failed", http.StatusBadGateway)
			return
		}
		defer upstream.Close()
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "HTTP/1 CONNECT required", http.StatusNotImplemented)
		return
	}
	conn, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	if !p.track(conn) {
		return
	}
	defer p.untrack(conn)
	if _, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if buffer.Flush() != nil {
		return
	}
	buffered := &bufferedConn{Conn: conn, reader: buffer.Reader}
	if upstream != nil {
		// Other hosts, including googlevideo.com, remain opaque end-to-end TLS.
		_ = conn.SetDeadline(time.Now().Add(time.Hour))
		_ = upstream.SetDeadline(time.Now().Add(time.Hour))
		finished := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, buffered)
			if c, ok := upstream.(*net.TCPConn); ok {
				_ = c.CloseWrite()
			}
			close(finished)
		}()
		_, _ = io.Copy(conn, upstream)
		_ = conn.Close()
		_ = upstream.Close()
		<-finished
		return
	}
	certificate, err := p.ca.leaf(host)
	if err != nil {
		return
	}
	secure := tls.Server(buffered, &tls.Config{Certificates: []tls.Certificate{*certificate}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	err = secure.HandshakeContext(ctx)
	cancel()
	if err != nil {
		p.tlsFailures.Add(1)
		p.lastError.Store("client_tls: certificate trust or TLS handshake failed")
		return
	}
	listener := newSingleListener(secure)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, inner *http.Request) { p.forward(w, inner, host) }), ReadHeaderTimeout: 15 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 5 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	defer server.Close()
	_ = network.ServeHTTP(server, listener, "YouTube AdBlock filtered connection")
}
func removeHop(headers http.Header) {
	for _, field := range strings.Split(headers.Get("Connection"), ",") {
		headers.Del(strings.TrimSpace(field))
	}
	for _, field := range []string{"Connection", "Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		headers.Del(field)
	}
}
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, host string) {
	requestHost := r.Host
	if h, port, err := net.SplitHostPort(requestHost); err == nil {
		if port != "443" {
			http.Error(w, "invalid host port", 400)
			return
		}
		requestHost = h
	}
	if !strings.EqualFold(requestHost, host) || (r.URL.IsAbs() && (!strings.EqualFold(r.URL.Host, host) || r.URL.Scheme != "https")) {
		http.Error(w, "CONNECT host mismatch", http.StatusMisdirectedRequest)
		return
	}
	if r.Method == http.MethodConnect || r.Header.Get("Upgrade") != "" {
		http.Error(w, "protocol upgrade unsupported on filtered hosts", http.StatusNotImplemented)
		return
	}
	out := r.Clone(r.Context())
	out.URL = &url.URL{Scheme: "https", Host: host, Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery}
	out.Host = host
	out.RequestURI = ""
	out.Header = r.Header.Clone()
	removeHop(out.Header)
	out.Header.Set("Accept-Encoding", "identity")
	p.requests.Add(1)
	response, err := p.transport.RoundTrip(out)
	if err != nil {
		p.upstreamFailures.Add(1)
		var verify *tls.CertificateVerificationError
		var unknown x509.UnknownAuthorityError
		var hostname x509.HostnameError
		if errors.As(err, &verify) || errors.As(err, &unknown) || errors.As(err, &hostname) {
			p.lastError.Store("upstream_tls: certificate validation failed")
		} else {
			var networkError net.Error
			if errors.As(err, &networkError) && networkError.Timeout() {
				p.lastError.Store("upstream_timeout: upstream request timed out")
			} else {
				p.lastError.Store("upstream_network: upstream connection failed")
			}
		}
		http.Error(w, "upstream HTTPS request failed", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	timer := time.NewTimer(time.Duration(p.options.QueueMS) * time.Millisecond)
	select {
	case p.filterSlots <- struct{}{}:
		changed, reason := filterResponseReason(response, out.URL.Path, p.options.BodyMiB<<20)
		if changed {
			p.filtered.Add(1)
		} else {
			p.unchanged.Add(1)
			switch reason {
			case "malformed", "read_error":
				p.malformed.Add(1)
			case "oversized":
				p.oversized.Add(1)
			case "unsupported_encoding":
				p.unsupported.Add(1)
			}
		}
		<-p.filterSlots
	case <-timer.C:
		p.skippedBusy.Add(1)
	case <-r.Context().Done():
		timer.Stop()
		return
	}
	timer.Stop()

	removeHop(response.Header)
	// HTTP/3 must not bypass a client configured to use this TCP CONNECT proxy.
	response.Header.Del("Alt-Svc")
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(data []byte) (int, error) { return c.reader.Read(data) }

type singleListener struct {
	conn net.Conn
	mu   sync.Mutex
	used bool
	done chan struct{}
	once sync.Once
}
type signalingConn struct {
	net.Conn
	listener *singleListener
}

func (c *signalingConn) Close() error {
	err := c.Conn.Close()
	c.listener.once.Do(func() { close(c.listener.done) })
	return err
}
func newSingleListener(c net.Conn) *singleListener {
	return &singleListener{conn: c, done: make(chan struct{})}
}
func (l *singleListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.used {
		l.used = true
		l.mu.Unlock()
		return &signalingConn{l.conn, l}, nil
	}
	l.mu.Unlock()
	<-l.done
	return nil, net.ErrClosed
}
func (l *singleListener) Close() error   { l.once.Do(func() { close(l.done) }); return l.conn.Close() }
func (l *singleListener) Addr() net.Addr { return l.conn.LocalAddr() }

// Run uses a loopback listener. Reach it through an authenticated VPN inbound
// or an SSH tunnel; no unauthenticated public interception proxy is opened.
func Run(ctx context.Context, listen, caDir string) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("youtube-proxy must listen on a loopback IP")
	}
	p, err := New(caDir)
	if err != nil {
		return err
	}
	defer p.Close()
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: p.handler(caDir), ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			p.Close()
			_ = server.Close()
		case <-done:
		}
	}()
	return network.ServeHTTP(server, listener, "YouTube AdBlock proxy")
}

func (p *Proxy) handler(caDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			p.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		switch r.URL.Path {
		case "/ca-cert.pem":
			data, err := os.ReadFile(filepath.Join(caDir, "ca-cert.pem"))
			if err != nil {
				http.Error(w, "certificate unavailable", 500)
				return
			}
			w.Header().Set("Content-Type", "application/x-pem-file")
			w.Header().Set("Content-Disposition", `attachment; filename="3x-ui-youtube-ca.pem"`)
			_, _ = w.Write(data)
		case "/status":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"interceptedHosts": []string{"youtube.com", "www.youtube.com", "m.youtube.com"}, "requests": p.requests.Load(), "filteredResponses": p.filtered.Load(), "tlsFailures": p.tlsFailures.Load(), "skippedBusy": p.skippedBusy.Load(), "experimental": true})
		default:
			http.NotFound(w, r)
		}
	})
}
