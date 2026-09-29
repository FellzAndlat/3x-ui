package tuic

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

func GetBinaryName() string {
	name := fmt.Sprintf("tuic-server-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func GetBinaryPath() string {
	custom := filepath.Join(config.GetBinFolderPath(), GetBinaryName())
	if _, err := os.Stat(custom); err == nil {
		return custom
	}
	binTuic := filepath.Join(config.GetBinFolderPath(), "tuic-server")
	if runtime.GOOS == "windows" {
		binTuic += ".exe"
	}
	if _, err := os.Stat(binTuic); err == nil {
		return binTuic
	}
	for _, p := range []string{"/usr/local/bin/tuic-server", "/usr/bin/tuic-server"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if path, err := exec.LookPath("tuic-server"); err == nil {
		return path
	}
	return binTuic
}

var (
	gracefulStopTimeout = 5 * time.Second
	forceStopTimeout    = 2 * time.Second
)

type procLogWriter struct {
	mu          sync.Mutex
	label       string
	buf         string
	lastLine    string
	uuidToEmail map[string]string
	lastActive  map[string]int64
	bindPeer    func(peer, email string) bool
}

func (w *procLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf += string(p)
	for {
		i := strings.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := w.buf[:i]
		w.buf = w.buf[i+1:]
		w.emitLocked(line)
	}
	return len(p), nil
}

func (w *procLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf != "" {
		line := w.buf
		w.buf = ""
		w.emitLocked(line)
	}
}

// tuicLogPeerForUUID extracts the QUIC peer address from tuic-server 1.0.0 log
// records of the form "[connection-id] [peer] [uuid] ...". The logger may add
// its own prefix, so parsing is anchored on the authenticated UUID rather than
// fixed field positions. The loop also handles an IPv6 peer wrapped by the
// server's outer brackets, e.g. "[[::1]:12345]".
func tuicLogPeerForUUID(line, uuid string) string {
	if uuid == "" {
		return ""
	}
	lowerLine := strings.ToLower(line)
	marker := "[" + strings.ToLower(uuid) + "]"
	idx := strings.Index(lowerLine, marker)
	if idx < 0 {
		return ""
	}
	prefix := strings.TrimSpace(line[:idx])
	end := strings.LastIndex(prefix, "]")
	if end < 0 {
		return ""
	}
	search := prefix[:end]
	for {
		start := strings.LastIndex(search, "[")
		if start < 0 {
			return ""
		}
		candidate := strings.TrimSpace(prefix[start+1 : end])
		if _, _, err := net.SplitHostPort(candidate); err == nil {
			return candidate
		}
		search = search[:start]
	}
}

func (w *procLogWriter) emitLocked(line string) {
	trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
	if trimmed == "" {
		return
	}
	w.lastLine = trimmed
	logger.Infof("tuic: tuic-server %s | %s", w.label, trimmed)

	now := time.Now().UnixMilli()
	lowerLine := strings.ToLower(line)
	for uuid, email := range w.uuidToEmail {
		if uuid == "" || email == "" || !strings.Contains(lowerLine, strings.ToLower(uuid)) {
			continue
		}
		if w.lastActive == nil {
			w.lastActive = make(map[string]int64)
		}
		w.lastActive[email] = now
		if w.bindPeer != nil {
			if peer := tuicLogPeerForUUID(line, uuid); peer != "" {
				w.bindPeer(peer, email)
			}
		}
	}
}

func (w *procLogWriter) LastLine() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastLine
}

type Process struct {
	mu              sync.RWMutex
	cmd             *exec.Cmd
	done            chan struct{}
	configPath      string
	logWriter       *procLogWriter
	exitErr         error
	intentionalStop atomic.Bool
}

// newProcess accepts the peer binder variadically to keep package-local tests
// and older call sites source-compatible while the TUIC relay learns per-client
// attribution.
func newProcess(configPath, label string, uuidToEmail map[string]string, binders ...func(string, string) bool) *Process {
	var bindPeer func(string, string) bool
	if len(binders) > 0 {
		bindPeer = binders[0]
	}
	return &Process{
		configPath: configPath,
		logWriter: &procLogWriter{
			label:       label,
			uuidToEmail: uuidToEmail,
			lastActive:  make(map[string]int64),
			bindPeer:    bindPeer,
		},
	}
}

func (p *Process) GetActiveEmails(window time.Duration) []string {
	if p == nil || p.logWriter == nil {
		return nil
	}
	p.logWriter.mu.Lock()
	defer p.logWriter.mu.Unlock()
	cutoff := time.Now().Add(-window).UnixMilli()
	var active []string
	for email, last := range p.logWriter.lastActive {
		if last >= cutoff {
			active = append(active, email)
		}
	}
	return active
}

func (p *Process) UpdateClients(uuidToEmail map[string]string) {
	if p == nil || p.logWriter == nil {
		return
	}
	p.logWriter.mu.Lock()
	defer p.logWriter.mu.Unlock()
	p.logWriter.uuidToEmail = uuidToEmail
}

func (p *Process) IsRunning() bool {
	p.mu.RLock()
	cmd, done := p.cmd, p.done
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return false
	}
	if done != nil {
		select {
		case <-done:
			return false
		default:
		}
	}
	return true
}

func (p *Process) GetResult() string {
	if line := p.logWriter.LastLine(); line != "" {
		return line
	}
	p.mu.RLock()
	exitErr := p.exitErr
	p.mu.RUnlock()
	if exitErr != nil {
		return exitErr.Error()
	}
	return ""
}

func (p *Process) Start() error {
	if p.IsRunning() {
		return errors.New("tuic-server is already running")
	}
	cmd := exec.CommandContext(context.Background(), GetBinaryPath(), "-c", p.configPath)
	cmd.Stdout = p.logWriter
	cmd.Stderr = p.logWriter
	done := make(chan struct{})
	p.mu.Lock()
	p.cmd = cmd
	p.done = done
	p.exitErr = nil
	p.mu.Unlock()
	p.intentionalStop.Store(false)
	if err := cmd.Start(); err != nil {
		close(done)
		p.mu.Lock()
		p.cmd = nil
		p.mu.Unlock()
		return err
	}
	attachChildLifetime(cmd)
	go p.wait(cmd, done)
	return nil
}

func (p *Process) wait(cmd *exec.Cmd, done chan struct{}) {
	defer close(done)
	err := cmd.Wait()
	p.logWriter.Flush()
	if err == nil || p.intentionalStop.Load() {
		return
	}
	if runtime.GOOS == "windows" {
		if strings.Contains(strings.ToLower(err.Error()), "exit status 1") {
			p.setExitErr(err)
			return
		}
	}
	logger.Errorf("tuic: tuic-server process exited: %v", err)
	p.setExitErr(err)
}

func (p *Process) setExitErr(err error) {
	p.mu.Lock()
	p.exitErr = err
	p.mu.Unlock()
}

func (p *Process) Stop() error {
	if !p.IsRunning() {
		return errors.New("tuic-server is not running")
	}
	p.intentionalStop.Store(true)
	p.mu.RLock()
	cmd, done := p.cmd, p.done
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return errors.New("tuic-server is not running")
	}

	if runtime.GOOS == "windows" {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		return waitForExit(done, forceStopTimeout)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return waitForExit(done, forceStopTimeout)
		}
		return err
	}

	if err := waitForExit(done, gracefulStopTimeout); err == nil {
		return nil
	}

	logger.Warning("tuic: tuic-server did not stop after SIGTERM, killing process")
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return waitForExit(done, forceStopTimeout)
}

func waitForExit(done <-chan struct{}, timeout time.Duration) error {
	if done == nil {
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return fmt.Errorf("timed out waiting for tuic-server process to stop after %s", timeout)
	}
}
