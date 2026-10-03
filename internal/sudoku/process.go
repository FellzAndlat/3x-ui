package sudoku

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

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

const (
	configTestTimeout = 10 * time.Second
	startupGrace      = 100 * time.Millisecond
)

type Process struct {
	opMu            sync.Mutex
	mu              sync.RWMutex
	cmd             *exec.Cmd
	done            chan struct{}
	configPath      string
	label           string
	exitErr         error
	intentionalStop atomic.Bool
}

func NewProcess(configPath, label string) *Process {
	return &Process{configPath: configPath, label: label}
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

func (p *Process) Start(binary string) error {
	p.opMu.Lock()
	defer p.opMu.Unlock()

	if p.IsRunning() {
		return errors.New("Sudoku is already running")
	}
	if err := validateConfig(binary, p.configPath); err != nil {
		return err
	}

	cmd := exec.CommandContext(context.Background(), binary, "-c", p.configPath)
	cmd.Env = append(os.Environ(), "SUDOKU_LOG_NO_TIMESTAMP=true")
	lw := &logWriter{label: p.label}
	cmd.Stdout = lw
	cmd.Stderr = lw
	done := make(chan struct{})
	p.intentionalStop.Store(false)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Sudoku: %w", err)
	}
	p.mu.Lock()
	p.cmd, p.done, p.exitErr = cmd, done, nil
	p.mu.Unlock()
	go p.wait(cmd, done)

	timer := time.NewTimer(startupGrace)
	defer timer.Stop()
	select {
	case <-done:
		p.mu.RLock()
		err := p.exitErr
		p.mu.RUnlock()
		if err != nil {
			return fmt.Errorf("Sudoku exited during startup: %w", err)
		}
		return errors.New("Sudoku exited during startup")
	case <-timer.C:
		return nil
	}
}

func validateConfig(binary, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), configTestTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "-test", "-c", path)
	cmd.Env = append(os.Environ(), "SUDOKU_LOG_NO_TIMESTAMP=true")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("Sudoku config validation timed out after %s: %w", configTestTimeout, ctx.Err())
	}
	if err == nil {
		return nil
	}

	detail := strings.TrimSpace(string(output))
	if len(detail) > 2048 {
		detail = detail[len(detail)-2048:]
	}
	if detail != "" {
		return fmt.Errorf("Sudoku config validation failed: %w: %s", err, detail)
	}
	return fmt.Errorf("Sudoku config validation failed: %w", err)
}

func (p *Process) wait(cmd *exec.Cmd, done chan struct{}) {
	defer close(done)
	err := cmd.Wait()
	if err == nil || p.intentionalStop.Load() {
		return
	}
	logger.Errorf("sudoku: %s exited: %v", p.label, err)
	p.mu.Lock()
	if p.cmd == cmd {
		p.exitErr = err
	}
	p.mu.Unlock()
}

func (p *Process) Stop() error {
	p.opMu.Lock()
	defer p.opMu.Unlock()

	p.mu.RLock()
	cmd, done := p.cmd, p.done
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if done != nil {
		select {
		case <-done:
			return nil
		default:
		}
	}

	p.intentionalStop.Store(true)
	if runtime.GOOS == "windows" {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("kill Sudoku process: %w", err)
		}
		return waitForExit(done, 2*time.Second)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("signal Sudoku process: %w", err)
	}
	if err := waitForExit(done, 5*time.Second); err == nil {
		return nil
	}
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill Sudoku process after graceful shutdown timeout: %w", err)
	}
	return waitForExit(done, 2*time.Second)
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
		return fmt.Errorf("timed out waiting for Sudoku process after %s", timeout)
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
		idx := len(w.buf)
		for i, ch := range w.buf {
			if ch == '\n' {
				idx = i
				break
			}
		}
		if idx == len(w.buf) {
			break
		}
		line := w.buf[:idx]
		w.buf = w.buf[idx+1:]
		if line != "" {
			logger.Infof("sudoku: %s | %s", w.label, line)
		}
	}
	return len(p), nil
}

func configDir(binDir string) string {
	return filepath.Join(binDir, "sudoku")
}

func configPath(binDir string, inboundID int) string {
	return filepath.Join(configDir(binDir), fmt.Sprintf("sudoku-%d.json", inboundID))
}

func keyPath(binDir string, inboundID int) string {
	return filepath.Join(configDir(binDir), fmt.Sprintf("sudoku-%d.key", inboundID))
}
