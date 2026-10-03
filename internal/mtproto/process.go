// Package mtproto manages Telemt sidecar processes for MTProto inbounds.
// Xray-core has no native MTProto inbound, therefore each 3x-ui MTProto inbound
// is served by a dedicated Telemt process with its own generated TOML and local
// control API. User/config changes are reloaded through Telemt's native API.
package mtproto

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	name := "telemt"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func GetBinaryPath() string         { return config.GetBinFolderPath() + "/" + GetBinaryName() }
func configDir() string             { return config.GetBinFolderPath() + "/mtproto" }
func configPathForID(id int) string { return fmt.Sprintf("%s/telemt-%d.toml", configDir(), id) }

var (
	gracefulStopTimeout = 5 * time.Second
	forceStopTimeout    = 2 * time.Second
)

type procLogWriter struct {
	mu         sync.Mutex
	label, buf string
	lastLine   string
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

func (w *procLogWriter) emitLocked(line string) {
	trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
	if trimmed == "" {
		return
	}
	w.lastLine = trimmed
	logger.Infof("mtproto: telemt %s | %s", w.label, trimmed)
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

func newProcess(configPath, label string) *Process {
	return &Process{configPath: configPath, logWriter: &procLogWriter{label: label}}
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
		return errors.New("telemt is already running")
	}
	// Current Telemt CLI accepts `run <config.toml>`; using the explicit run
	// subcommand avoids daemonizing and keeps the child under x-ui supervision.
	cmd := exec.CommandContext(context.Background(), GetBinaryPath(), "run", p.configPath)
	cmd.Stdout, cmd.Stderr = p.logWriter, p.logWriter
	done := make(chan struct{})
	p.mu.Lock()
	p.cmd, p.done, p.exitErr = cmd, done, nil
	p.mu.Unlock()
	p.intentionalStop.Store(false)
	if err := cmd.Start(); err != nil {
		close(done)
		p.mu.Lock()
		p.cmd = nil
		p.mu.Unlock()
		// The config describes a port that is only valid while this Telemt
		// sidecar is alive. Do not leave a dead generated config behind for
		// the MEKO port discovery to treat as an active inbound.
		_ = os.Remove(p.configPath)
		scheduleMekoSync()
		return err
	}
	attachChildLifetime(cmd)
	go p.wait(cmd, done)
	scheduleMekoSync()
	return nil
}

func (p *Process) wait(cmd *exec.Cmd, done chan struct{}) {
	defer close(done)
	err := cmd.Wait()
	p.logWriter.Flush()
	if err == nil || p.intentionalStop.Load() {
		return
	}
	logger.Errorf("mtproto: telemt process exited: %v", err)
	p.setExitErr(err)
	// A sidecar that dies unexpectedly no longer owns its configured port.
	// Remove its generated config so MEKO cannot keep firewall rules for a
	// listener that is no longer running. Reconcile will regenerate it before
	// the next start attempt.
	_ = os.Remove(p.configPath)
	scheduleMekoSync()
}

func (p *Process) setExitErr(err error) {
	p.mu.Lock()
	p.exitErr = err
	p.mu.Unlock()
}

func (p *Process) Stop() error {
	if !p.IsRunning() {
		return errors.New("telemt is not running")
	}
	// The caller may remove or replace this sidecar's TOML immediately after
	// Stop returns. Debouncing the MEKO refresh lets the helper observe that
	// final config set instead of briefly re-applying the old port.
	defer scheduleMekoSync()
	p.intentionalStop.Store(true)
	p.mu.RLock()
	cmd, done := p.cmd, p.done
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return errors.New("telemt is not running")
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
	logger.Warning("mtproto: telemt did not stop after SIGTERM, killing process")
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return waitForExit(done, forceStopTimeout)
}

func waitForExit(done <-chan struct{}, timeout time.Duration) error {
	if done == nil {
		return nil
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-done:
		return nil
	case <-t.C:
		return fmt.Errorf("timed out waiting for telemt process to stop after %s", timeout)
	}
}
