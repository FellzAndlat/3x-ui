package singbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	gprocess "github.com/shirou/gopsutil/v4/process"
)

const (
	defaultGracefulStopTimeout = 5 * time.Second
	defaultForceStopTimeout    = 2 * time.Second
	defaultCommandTimeout      = 10 * time.Second
)

func GetBinaryName() string {
	arch := runtime.GOARCH
	if arch == "arm" {
		arch = "armv7"
	}
	return fmt.Sprintf("sing-box-%s-%s", runtime.GOOS, arch)
}

func resolveManagedPath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	base := filepath.Dir(configExecutablePath())
	return filepath.Join(base, path)
}

func GetBinaryPath() string {
	return resolveManagedPath(filepath.Join(config.GetBinFolderPath(), GetBinaryName()))
}

func GetConfigPath() string {
	return resolveManagedPath(filepath.Join(config.GetBinFolderPath(), "sing-box.json"))
}

// resolvePath is kept as a small compatibility helper for callers that pass
// an already-managed relative path.
func resolvePath(path string) string {
	return resolveManagedPath(path)
}

func configExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return exe
}

type Process struct {
	mu          sync.RWMutex
	lifecycle   sync.Mutex
	cmd         *exec.Cmd
	done        chan struct{}
	exitErr     error
	version     string
	startTime   time.Time
	config      string
	externalPID int
	isolated    bool
}

func NewProcess(configPath string) *Process {
	return &Process{config: resolvePath(configPath), version: "Unknown"}
}

// NewTestProcess never discovers or stops the managed production process.
func NewTestProcess(configPath string) *Process {
	return &Process{config: resolvePath(configPath), version: "Unknown", isolated: true}
}

func (p *Process) IsRunning() bool {
	p.mu.RLock()
	cmd, done, externalPID := p.cmd, p.done, p.externalPID
	p.mu.RUnlock()
	if cmd != nil && cmd.Process != nil {
		running := false
		if done != nil {
			select {
			case <-done:
			default:
				running = true
			}
		} else {
			running = true
		}
		if running {
			p.clearErrorWhileRunning()
			return true
		}
	}
	if p.isolated {
		return false
	}
	if externalPID > 0 {
		if processMatchesBinary(externalPID, GetBinaryPath()) {
			p.clearErrorWhileRunning()
			return true
		}
		p.clearExternalPID(externalPID)
	}
	if pid := p.managedPID(); pid > 0 {
		p.mu.Lock()
		p.externalPID = pid
		p.exitErr = nil
		p.mu.Unlock()
		return true
	}
	return false
}

func (p *Process) clearExternalPID(pid int) {
	p.mu.Lock()
	if p.externalPID == pid {
		p.externalPID = 0
	}
	p.mu.Unlock()
}

func (p *Process) clearErrorWhileRunning() {
	p.mu.Lock()
	p.exitErr = nil
	p.mu.Unlock()
}

func (p *Process) GetErr() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.exitErr
}

func (p *Process) SetError(err error) {
	p.setErr(err)
}

func (p *Process) GetVersion() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.version
}

func (p *Process) SupportsNativeAPI() bool {
	version := strings.TrimSpace(strings.TrimPrefix(p.GetVersion(), "v"))
	fields := strings.Fields(version)
	if len(fields) == 0 {
		return false
	}
	parts := strings.Split(fields[0], ".")
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > 1 || (major == 1 && minor >= 14)
}

func (p *Process) GetStartTime() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.startTime
}

// GetUptime returns the uptime of the sing-box process in seconds.
func (p *Process) GetUptime() uint64 {
	p.mu.RLock()
	startTime, externalPID, cmd, done := p.startTime, p.externalPID, p.cmd, p.done
	p.mu.RUnlock()

	if !startTime.IsZero() && cmd != nil && cmd.Process != nil {
		running := true
		if done != nil {
			select {
			case <-done:
				running = false
			default:
			}
		}
		if running {
			return uint64(time.Since(startTime).Seconds())
		}
	}
	if p.isolated {
		return 0
	}
	if externalPID > 0 && !processMatchesBinary(externalPID, GetBinaryPath()) {
		p.clearExternalPID(externalPID)
		externalPID = 0
	}
	if externalPID <= 0 {
		externalPID = findRunningPID(GetBinaryPath())
		if externalPID > 0 {
			p.mu.Lock()
			p.externalPID = externalPID
			p.mu.Unlock()
		}
	}
	if externalPID <= 0 {
		return 0
	}
	proc, err := gprocess.NewProcess(int32(externalPID))
	if err != nil {
		return 0
	}
	createdMs, err := proc.CreateTime()
	if err != nil || createdMs <= 0 {
		return 0
	}
	started := time.UnixMilli(createdMs)
	if started.After(time.Now()) {
		return 0
	}
	return uint64(time.Since(started).Seconds())
}

func (p *Process) ConfigPath() string { return p.config }

func (p *Process) Validate(ctx context.Context) error {
	binary := resolvePath(GetBinaryPath())
	cfg := resolvePath(p.config)
	if _, err := os.Stat(binary); err != nil {
		err = fmt.Errorf("sing-box binary is not installed: %w", err)
		p.setErr(err)
		return err
	}
	if _, err := os.Stat(cfg); err != nil {
		err = fmt.Errorf("sing-box config does not exist: %w", err)
		p.setErr(err)
		return err
	}
	// Snell requires 1.14 in both directions. The binary check remains authoritative.
	if data, err := os.ReadFile(cfg); err == nil && bytes.Contains(data, []byte(`"snell"`)) {
		if _, err := p.Version(ctx); err != nil {
			return err
		}
		if !p.SupportsNativeAPI() {
			return fmt.Errorf("Snell requires sing-box 1.14.0 or newer; installed: %s", p.GetVersion())
		}
	}
	cmdCtx, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, binary, "check", "-c", cfg)
	cmd.Dir = filepath.Dir(binary)
	output, err := cmd.CombinedOutput()
	if err != nil {
		err = fmt.Errorf("sing-box config check failed: %w: %s", err, strings.TrimSpace(string(output)))
		p.setErr(err)
		return err
	}
	p.setErr(nil)
	return nil
}

func (p *Process) Version(ctx context.Context) (string, error) {
	binary := resolvePath(GetBinaryPath())
	cmdCtx, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, binary, "version")
	cmd.Dir = filepath.Dir(binary)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("sing-box version failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	version := strings.TrimSpace(string(output))
	if lines := strings.SplitN(version, "\n", 2); len(lines) > 0 {
		version = strings.TrimSpace(strings.TrimPrefix(lines[0], "sing-box version"))
	}
	p.mu.Lock()
	p.version = version
	p.mu.Unlock()
	return version, nil
}

func (p *Process) Start(ctx context.Context) error {
	p.lifecycle.Lock()
	defer p.lifecycle.Unlock()
	return p.startLocked(ctx)
}

func (p *Process) startLocked(ctx context.Context) error {
	p.mu.RLock()
	running, done := p.cmd != nil && p.cmd.Process != nil, p.done
	p.mu.RUnlock()
	if running {
		select {
		case <-done:
		default:
			return nil
		}
	}
	if pid := p.managedPID(); pid > 0 {
		p.mu.Lock()
		p.externalPID = pid
		p.mu.Unlock()
		return nil
	}
	if err := p.Validate(ctx); err != nil {
		return err
	}
	binary, cfg := resolvePath(GetBinaryPath()), resolvePath(p.config)
	// The core must outlive the HTTP request that started it.
	cmd := exec.CommandContext(context.Background(), binary, "run", "-c", cfg)
	cmd.Dir = filepath.Dir(binary)
	output := &processOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		err = fmt.Errorf("start sing-box: %w", err)
		p.setErr(err)
		return err
	}
	p.mu.Lock()
	p.cmd = cmd
	p.done = make(chan struct{})
	p.exitErr = nil
	p.externalPID = 0
	p.startTime = time.Now()
	done = p.done
	p.mu.Unlock()
	go func() {
		err := cmd.Wait()
		if err != nil {
			if details := output.String(); details != "" {
				err = fmt.Errorf("%w: %s", err, details)
			}
		}
		p.mu.Lock()
		p.exitErr = err
		close(done)
		p.mu.Unlock()
	}()
	return nil
}

func (p *Process) setErr(err error) {
	p.mu.Lock()
	p.exitErr = err
	p.mu.Unlock()
}

func (p *Process) Stop() error {
	p.lifecycle.Lock()
	defer p.lifecycle.Unlock()
	return p.stopLocked()
}

func (p *Process) stopLocked() error {
	p.mu.RLock()
	cmd, done, externalPID := p.cmd, p.done, p.externalPID
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		binary := GetBinaryPath()
		if externalPID > 0 && !processMatchesBinary(externalPID, binary) {
			p.clearExternalPID(externalPID)
			externalPID = 0
		}
		if externalPID <= 0 && !p.isolated {
			externalPID = findRunningPID(binary)
		}
		if externalPID <= 0 {
			return nil
		}
		if !processMatchesBinary(externalPID, binary) {
			p.clearExternalPID(externalPID)
			return nil
		}
		proc, err := os.FindProcess(externalPID)
		if err != nil {
			return err
		}
		if err := proc.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
			// Never escalate a cached PID without verifying it still points at
			// the configured sing-box binary. The PID may have been reused after
			// the identity check above.
			if !processMatchesBinary(externalPID, binary) {
				p.clearExternalPID(externalPID)
				return nil
			}
			if killErr := proc.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				return fmt.Errorf("force stop external sing-box process %d: %w", externalPID, killErr)
			}
			if !p.waitForExternalExit(externalPID, defaultForceStopTimeout) {
				return fmt.Errorf("external sing-box process %d did not exit after force stop", externalPID)
			}
			p.setErr(nil)
			return nil
		}
		if p.waitForExternalExit(externalPID, defaultGracefulStopTimeout) {
			p.setErr(nil)
			return nil
		}
		// Graceful shutdown timed out. Revalidate immediately before SIGKILL so
		// a PID recycled during the wait cannot cause an unrelated process to be
		// terminated.
		if !processMatchesBinary(externalPID, binary) {
			p.clearExternalPID(externalPID)
			p.setErr(nil)
			return nil
		}
		proc, err = os.FindProcess(externalPID)
		if err != nil {
			return err
		}
		if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("force stop external sing-box process %d: %w", externalPID, err)
		}
		if !p.waitForExternalExit(externalPID, defaultForceStopTimeout) {
			return fmt.Errorf("external sing-box process %d did not exit after force stop", externalPID)
		}
		p.setErr(nil)
		return nil
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		_ = cmd.Process.Kill()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(defaultGracefulStopTimeout):
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(defaultForceStopTimeout):
			}
		}
	}
	p.setErr(nil)
	return nil
}

func (p *Process) Restart(ctx context.Context) error {
	p.lifecycle.Lock()
	defer p.lifecycle.Unlock()
	if err := p.Validate(ctx); err != nil {
		return err
	}
	if err := p.stopLocked(); err != nil {
		return err
	}
	return p.startLocked(ctx)
}

func (p *Process) waitForExternalExit(pid int, timeout time.Duration) bool {
	binary := GetBinaryPath()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processMatchesBinary(pid, binary) {
			p.clearExternalPID(pid)
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !processMatchesBinary(pid, binary) {
		p.clearExternalPID(pid)
		return true
	}
	return false
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return proc != nil
	}
	return proc.Signal(nil) == nil
}

func processMatchesBinary(pid int, binary string) bool {
	if pid <= 0 || runtime.GOOS != "linux" {
		return false
	}

	binary = filepath.Clean(resolvePath(binary))
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = filepath.Clean(resolved)
	}

	procDir := filepath.Join("/proc", strconv.Itoa(pid))
	if exe, err := os.Readlink(filepath.Join(procDir, "exe")); err == nil {
		exe = strings.TrimSuffix(exe, " (deleted)")
		return filepath.Clean(exe) == binary
	}

	// /proc/<pid>/exe can be unavailable under restrictive procfs settings.
	// Fall back to argv[0], but only when the stronger executable check could
	// not be read at all.
	cmdline, err := os.ReadFile(filepath.Join(procDir, "cmdline"))
	if err != nil {
		return false
	}
	args := strings.Split(string(cmdline), "\x00")
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return false
	}
	arg0 := filepath.Clean(resolvePath(args[0]))
	if resolved, err := filepath.EvalSymlinks(arg0); err == nil {
		arg0 = filepath.Clean(resolved)
	}
	return arg0 == binary
}

func findRunningPID(binary string) int {
	if runtime.GOOS != "linux" {
		return 0
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if processMatchesBinary(pid, binary) {
			return pid
		}
	}
	return 0
}

const processOutputLimit = 64 * 1024

type processOutput struct {
	mu  sync.Mutex
	buf []byte
}

func (w *processOutput) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(b) >= processOutputLimit {
		w.buf = append(w.buf[:0], b[len(b)-processOutputLimit:]...)
		return len(b), nil
	}
	w.buf = append(w.buf, b...)
	if len(w.buf) > processOutputLimit {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-processOutputLimit:]...)
	}
	return len(b), nil
}

func (w *processOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.TrimSpace(string(w.buf))
}

func (p *Process) managedPID() int {
	if p.isolated {
		return 0
	}
	return findRunningPID(GetBinaryPath())
}
