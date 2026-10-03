package mieru

import (
	"context"
	"errors"
	"fmt"
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
	n := fmt.Sprintf("mita-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return n
}

func GetBinaryPath() string {
	custom := filepath.Join(config.GetBinFolderPath(), GetBinaryName())
	if _, err := os.Stat(custom); err == nil {
		return custom
	}
	short := filepath.Join(config.GetBinFolderPath(), "mita")
	if runtime.GOOS == "windows" {
		short += ".exe"
	}
	if _, err := os.Stat(short); err == nil {
		return short
	}
	for _, p := range []string{"/usr/local/bin/mita", "/usr/bin/mita"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("mita"); err == nil {
		return p
	}
	return short
}

type Process struct {
	mu                            sync.RWMutex
	cmd                           *exec.Cmd
	done                          chan struct{}
	configPath, label, socketPath string
	exitErr                       error
	intentionalStop               atomic.Bool
}

func newProcess(path, label, socket string) *Process {
	return &Process{configPath: path, label: label, socketPath: socket}
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

func (p *Process) Start() error {
	if p.IsRunning() {
		return errors.New("mita is already running")
	}
	cmd := exec.CommandContext(context.Background(), GetBinaryPath(), "run")
	socketPath := p.socketPath
	configPath := p.configPath
	if abs, err := filepath.Abs(socketPath); err == nil {
		socketPath = abs
	}
	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	cmd.Env = append(os.Environ(), "MITA_CONFIG_JSON_FILE="+configPath, "MITA_UDS_PATH="+socketPath, "MITA_INSECURE_UDS=true", "MITA_LOG_NO_TIMESTAMP=true")
	lw := &logWriter{label: p.label}
	cmd.Stdout = lw
	cmd.Stderr = lw
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
		return err
	}
	go p.wait(cmd, done)
	return nil
}

func (p *Process) wait(cmd *exec.Cmd, done chan struct{}) {
	defer close(done)
	err := cmd.Wait()
	if err == nil || p.intentionalStop.Load() {
		return
	}
	logger.Errorf("mieru: mita %s exited: %v", p.label, err)
	p.mu.Lock()
	p.exitErr = err
	p.mu.Unlock()
}

func (p *Process) Reload() error {
	if !p.IsRunning() {
		return errors.New("mita is not running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	socketPath := p.socketPath
	configPath := p.configPath
	if abs, err := filepath.Abs(socketPath); err == nil {
		socketPath = abs
	}
	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	cmd := exec.CommandContext(ctx, GetBinaryPath(), "reload")
	cmd.Env = append(os.Environ(),
		"MITA_CONFIG_JSON_FILE="+configPath,
		"MITA_UDS_PATH="+socketPath,
		"MITA_INSECURE_UDS=true",
		"MITA_LOG_NO_TIMESTAMP=true",
	)
	lw := &logWriter{label: p.label + " reload"}
	cmd.Stdout, cmd.Stderr = lw, lw
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("mita reload timed out: %w", ctx.Err())
		}
		return fmt.Errorf("mita reload failed: %w", err)
	}
	return nil
}

func (p *Process) Stop() error {
	if !p.IsRunning() {
		return errors.New("mita is not running")
	}
	p.intentionalStop.Store(true)
	p.mu.RLock()
	cmd, done := p.cmd, p.done
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return errors.New("mita is not running")
	}
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
		return waitForExit(done, 2*time.Second)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	if err := waitForExit(done, 5*time.Second); err == nil {
		return nil
	}
	_ = cmd.Process.Kill()
	return waitForExit(done, 2*time.Second)
}

func waitForExit(done <-chan struct{}, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return fmt.Errorf("timed out waiting for process after %s", timeout)
	}
}

type logWriter struct {
	mu    sync.Mutex
	label string
	buf   string
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf += string(p)
	for {
		idx := strings.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSpace(w.buf[:idx])
		w.buf = w.buf[idx+1:]
		if line != "" {
			logger.Infof("mieru: mita %s | %s", w.label, line)
		}
	}
	return len(p), nil
}
