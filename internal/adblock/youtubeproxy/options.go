package youtubeproxy

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/net/proxy"
)

type Options struct {
	MaxConnections int    `json:"maxConnections"`
	Workers        int    `json:"workers"`
	BodyMiB        int    `json:"bodyMiB"`
	QueueMS        int    `json:"queueMs"`
	SOCKSAddress   string `json:"-"`
}

func DefaultOptions() Options { return Options{128, 2, 8, 250, ""} }
func (o Options) Validate() error {
	if o.MaxConnections < 8 || o.MaxConnections > 512 || o.Workers < 1 || o.Workers > 8 || o.BodyMiB < 1 || o.BodyMiB > 16 || o.QueueMS < 0 || o.QueueMS > 2000 {
		return fmt.Errorf("limits: connections 8–512, workers 1–8, body 1–16 MiB, queue 0–2000 ms")
	}
	if o.Workers*o.BodyMiB > 32 {
		return fmt.Errorf("combined rewrite buffers must not exceed 32 MiB before decoding")
	}
	return nil
}

type Counters struct {
	Malformed        uint64 `json:"malformed"`
	Oversized        uint64 `json:"oversized"`
	Unsupported      uint64 `json:"unsupported"`
	Listen           string `json:"listen"`
	Running          bool   `json:"running"`
	Requests         uint64 `json:"requests"`
	Filtered         uint64 `json:"filtered"`
	Busy             uint64 `json:"busy"`
	TLSFailures      uint64 `json:"tlsFailures"`
	UpstreamFailures uint64 `json:"upstreamFailures"`
	Unchanged        uint64 `json:"unchanged"`
	LastError        string `json:"lastError"`
}

func (p *Proxy) Stats() Counters {
	message, _ := p.lastError.Load().(string)
	return Counters{Malformed: p.malformed.Load(), Oversized: p.oversized.Load(), Unsupported: p.unsupported.Load(), Running: true, Requests: p.requests.Load(), Filtered: p.filtered.Load(), Busy: p.skippedBusy.Load(), TLSFailures: p.tlsFailures.Load(), UpstreamFailures: p.upstreamFailures.Load(), Unchanged: p.unchanged.Load(), LastError: message}
}
func NewWithOptions(dir string, options Options) (*Proxy, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	p, err := New(dir)
	if err != nil {
		return nil, err
	}
	p.options = options
	p.permits = make(chan struct{}, options.MaxConnections)
	p.filterSlots = make(chan struct{}, options.Workers)
	if options.SOCKSAddress != "" {
		host, _, err := net.SplitHostPort(options.SOCKSAddress)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			p.Close()
			return nil, fmt.Errorf("upstream bridge must be loopback")
		}
		d, err := proxy.SOCKS5("tcp", options.SOCKSAddress, nil, &net.Dialer{Timeout: 15 * time.Second})
		if err != nil {
			p.Close()
			return nil, err
		}
		contextual, ok := d.(proxy.ContextDialer)
		if !ok {
			p.Close()
			return nil, fmt.Errorf("upstream lacks context dialer")
		}
		p.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return contextual.DialContext(ctx, network, address)
		}
	}
	return p, nil
}
