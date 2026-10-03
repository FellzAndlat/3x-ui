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
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	utls "github.com/refraction-networking/utls"
)

var fptnMetricPattern = regexp.MustCompile(`^fptn_user_(incoming|outgoing)_traffic_bytes\{([^}]*)\}\s+([0-9.eE+\-]+)$`)
var fptnLabelPattern = regexp.MustCompile(`(?:^|,)\s*(username|session_id)="([a-zA-Z0-9_-]+)"`)

type fptnTrafficSample struct {
	email string
	trafficCounters
}

func (m *Manager) collectAdditionalTraffic(proc *running) []TrafficDelta {
	if proc.protocol == model.OpenFlux {
		return collectOpenFluxTraffic(proc)
	}
	rows, err := collectFPTNTraffic(proc)
	if err != nil {
		return nil
	} // Keep the previous baseline after a failed poll.
	byEmail := map[string]TrafficDelta{}
	// Baselines belong to upstream session labels. Summing counters before
	// differencing loses traffic when sessions reset at different times.
	for session, row := range rows {
		old := proc.counters[session]
		up, down := cumulativeTrafficDelta(row.up, old.up), cumulativeTrafficDelta(row.down, old.down)
		delta := byEmail[row.email]
		delta.Tag, delta.Email = proc.tag, row.email
		delta.Up = saturatedTrafficSum(delta.Up, up)
		delta.Down = saturatedTrafficSum(delta.Down, down)
		delta.Active = delta.Up > 0 || delta.Down > 0
		byEmail[row.email] = delta
		proc.counters[session] = row.trafficCounters
	}
	for session := range proc.counters {
		if _, present := rows[session]; !present {
			delete(proc.counters, session)
		}
	}
	var deltas []TrafficDelta
	for _, delta := range byEmail {
		if delta.Active {
			deltas = append(deltas, delta)
		}
	}

	return deltas
}

func collectFPTNTraffic(proc *running) (map[string]fptnTrafficSample, error) {
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
	rows := map[string]fptnTrafficSample{}
	sc := bufio.NewScanner(io.LimitReader(response.Body, 4<<20))
	sc.Buffer(make([]byte, 4096), 64<<10)
	for sc.Scan() {
		match := fptnMetricPattern.FindStringSubmatch(sc.Text())
		if len(match) != 4 {
			continue
		}
		labels := map[string]string{}
		for _, label := range fptnLabelPattern.FindAllStringSubmatch(match[2], -1) {
			labels[label[1]] = label[2]
		}
		email := emails[labels["username"]]
		if email == "" {
			continue
		}
		v, err := strconv.ParseFloat(match[3], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v >= float64(^uint64(0)) {
			continue
		}
		session := strings.Join([]string{email, labels["session_id"]}, "\x00")
		row := rows[session]
		row.email = email
		// FPTN calls traffic sent TO CLIENT "incoming" and traffic FROM CLIENT
		// "outgoing". Reverse those names to match panel upload/download counters.
		if match[1] == "incoming" {
			row.down = uint64(v)
		} else {
			row.up = uint64(v)
		}
		rows[session] = row
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

func saturatedTrafficSum(a, b int64) int64 {
	if b > math.MaxInt64-a {
		return math.MaxInt64
	}
	return a + b
}
