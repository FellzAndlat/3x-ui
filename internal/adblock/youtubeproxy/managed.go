package youtubeproxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type Instance struct {
	alive   atomic.Bool
	Proxy   *Proxy
	Port    int
	Server  *http.Server
	Options Options
}

var managed struct {
	sync.Mutex
	active, candidate *Instance
}

func Prepare(dir string, options Options) (int, error) {
	managed.Lock()
	defer managed.Unlock()
	for _, instance := range []*Instance{managed.candidate, managed.active} {
		if instance != nil && instance.alive.Load() && instance.Options == options {
			if instance == managed.active && managed.candidate != nil {
				managed.candidate.Proxy.Close()
				_ = managed.candidate.Server.Close()
				managed.candidate = nil
			}
			return instance.Port, nil
		}
	}
	p, err := NewWithOptions(dir, options)
	if err != nil {
		return 0, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		p.Close()
		return 0, err
	}
	instance := &Instance{Proxy: p, Port: listener.Addr().(*net.TCPAddr).Port, Server: &http.Server{Handler: p.handler(dir), ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}, Options: options}
	instance.alive.Store(true)
	if managed.candidate != nil {
		managed.candidate.Proxy.Close()
		_ = managed.candidate.Server.Close()
	}
	managed.candidate = instance
	go func() {
		defer instance.alive.Store(false)
		if err := instance.Server.Serve(listener); err != nil && err != http.ErrServerClosed {
			p.lastError.Store("listener: server stopped")
		}
	}()
	return instance.Port, nil
}
func Commit(enabled bool) {
	managed.Lock()
	defer managed.Unlock()
	if !enabled {
		for _, i := range []*Instance{managed.active, managed.candidate} {
			if i != nil {
				i.Proxy.Close()
				_ = i.Server.Close()
			}
		}
		managed.active = nil
		managed.candidate = nil
		return
	}
	if managed.candidate != nil {
		old := managed.active
		managed.active = managed.candidate
		managed.candidate = nil
		if old != nil {
			old.Proxy.Close()
			_ = old.Server.Close()
		}
	}
}
func Abort() {
	managed.Lock()
	defer managed.Unlock()
	if managed.candidate != nil {
		managed.candidate.Proxy.Close()
		_ = managed.candidate.Server.Close()
		managed.candidate = nil
	}
}
func Status() Counters {
	managed.Lock()
	defer managed.Unlock()
	if managed.active != nil {
		status := managed.active.Proxy.Stats()
		status.Running = managed.active.alive.Load()
		status.Listen = "127.0.0.1:" + strconv.Itoa(managed.active.Port)
		return status
	}
	if managed.candidate != nil {
		status := managed.candidate.Proxy.Stats()
		status.Running = managed.candidate.alive.Load()
		status.Listen = "127.0.0.1:" + strconv.Itoa(managed.candidate.Port)
		return status
	}
	return Counters{}
}

type CertificateInfo struct {
	Fingerprint string `json:"fingerprint"`
	Expires     string `json:"expires"`
}

func Certificate(dir string) ([]byte, CertificateInfo, error) {
	a, err := loadAuthority(dir)
	if err != nil {
		return nil, CertificateInfo{}, err
	}
	sum := sha256.Sum256(a.cert.Raw)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.cert.Raw}), CertificateInfo{hex.EncodeToString(sum[:]), a.cert.NotAfter.UTC().Format(time.RFC3339)}, nil
}

func NeedsRecovery() bool {
	managed.Lock()
	defer managed.Unlock()
	return managed.active != nil && !managed.active.alive.Load() || managed.candidate != nil && !managed.candidate.alive.Load()
}
