package externalvpn

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1" // Upstream FPTN's TLS session marker, not a credential hash.
	"crypto/x509"
	encodingbinary "encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	utls "github.com/refraction-networking/utls"
)

var fptnMetricPattern = regexp.MustCompile(`^fptn_user_(incoming|outgoing)_traffic_bytes\{[^}]*username="([a-zA-Z0-9]+)"[^}]*\}\s+([0-9.eE+\-]+)$`)

func (m *Manager) collectAdditionalTraffic(proc *running) []TrafficDelta {
	if proc.protocol == model.OpenFlux {
		return collectOpenFluxTraffic(proc)
	}
	rows, err := collectFPTNTraffic(proc)
	if err != nil {
		return nil
	} // Keep the previous baseline after a failed poll.
	var deltas []TrafficDelta
	for email, row := range rows {
		old := proc.counters[email]
		up, down := cumulativeTrafficDelta(row.up, old.up), cumulativeTrafficDelta(row.down, old.down)
		if up > 0 || down > 0 {
			deltas = append(deltas, TrafficDelta{Tag: proc.tag, Email: email, Up: up, Down: down, Active: true})
		}
		proc.counters[email] = row
	}
	return deltas
}

func collectFPTNTraffic(proc *running) (map[string]trafficCounters, error) {
	inst := proc.instance
	cert := inst.Settings.Certificate
	if cert == "" {
		cert = filepath.Join(directory(), strconv.Itoa(inst.ID), "cert.pem")
	}
	data := []byte(inst.Settings.CertificatePEM)
	if inst.Settings.Certificate != "" || len(data) == 0 {
		var err error
		data, err = os.ReadFile(cert)
		if err != nil {
			return nil, err
		}
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("invalid FPTN CA")
	}
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false,
		DialTLSContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			tls := utls.UClient(conn, &utls.Config{RootCAs: roots, ServerName: inst.Settings.Hostname}, utls.HelloChrome_Auto)
			if err := tls.BuildHandshakeState(); err != nil {
				conn.Close()
				return nil, err
			}
			session := make([]byte, 32)
			if _, err := rand.Read(session); err != nil {
				conn.Close()
				return nil, err
			}
			var stamp [4]byte
			encodingbinary.BigEndian.PutUint32(stamp[:], uint32(time.Now().Unix()))
			hash := sha1.Sum(stamp[:])
			copy(session[28:], hash[:4])
			tls.HandshakeState.Hello.SessionId = session
			// Use HTTP/1.1 with net/http; its custom TLS dialer does not negotiate h2.
			for _, ext := range tls.Extensions {
				if alpn, ok := ext.(*utls.ALPNExtension); ok {
					alpn.AlpnProtocols = []string{"http/1.1"}
				}
			}
			if err := tls.MarshalClientHello(); err != nil {
				conn.Close()
				return nil, err
			}
			if err := tls.HandshakeContext(ctx); err != nil {
				conn.Close()
				return nil, err
			}
			return tls, nil
		},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	response, err := client.Get("https://" + proc.metricsAddr + "/api/v1/metrics/" + inst.Settings.MetricsKey)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FPTN metrics: %s", response.Status)
	}
	emails := map[string]string{}
	for _, c := range inst.Settings.Clients {
		emails[FPTNUsername(c.Email)] = c.Email
	}
	rows := map[string]trafficCounters{}
	sc := bufio.NewScanner(io.LimitReader(response.Body, 4<<20))
	sc.Buffer(make([]byte, 4096), 64<<10)
	for sc.Scan() {
		match := fptnMetricPattern.FindStringSubmatch(sc.Text())
		if len(match) != 4 {
			continue
		}
		email := emails[match[2]]
		if email == "" {
			continue
		}
		v, err := strconv.ParseFloat(match[3], 64)
		if err != nil || v < 0 || v >= float64(^uint64(0)) {
			continue
		}
		row := rows[email]
		// FPTN calls traffic sent TO CLIENT "incoming" and traffic FROM CLIENT
		// "outgoing". Reverse those names to match panel upload/download counters.
		if match[1] == "incoming" {
			row.down += uint64(v)
		} else {
			row.up += uint64(v)
		}
		rows[email] = row
	}
	return rows, sc.Err()
}

func collectOpenFluxTraffic(proc *running) []TrafficDelta {
	socket := filepath.Join(directory(), strconv.Itoa(proc.instance.ID), "ipc.sock")
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2500 * time.Millisecond))
	// Upstream broadcasts framed JSON status once a second. Bound every frame
	// and ignore cookie/log messages; no account cookies are persisted by panel.
	for i := 0; i < 16; i++ {
		var header [4]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return nil
		}
		size := encodingbinary.BigEndian.Uint32(header[:])
		if size < 1 || size > 1<<20 {
			return nil
		}
		frame := make([]byte, size)
		if _, err := io.ReadFull(conn, frame); err != nil {
			return nil
		}
		if frame[0] != 3 {
			continue
		}
		var status struct {
			Connected bool   `json:"connected"`
			In        uint64 `json:"bytes_in"`
			Out       uint64 `json:"bytes_out"`
		}
		if err := json.Unmarshal(frame[1:], &status); err != nil {
			return nil
		}
		for _, c := range proc.instance.Settings.Clients {
			if !c.Enable {
				continue
			}
			old := proc.counters[c.Email]
			up, down := cumulativeTrafficDelta(status.In, old.up), cumulativeTrafficDelta(status.Out, old.down)
			proc.counters[c.Email] = trafficCounters{up: status.In, down: status.Out}
			return []TrafficDelta{{Tag: proc.tag, Email: c.Email, Up: up, Down: down, Active: status.Connected}}
		}
	}
	return nil
}
