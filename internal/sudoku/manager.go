package sudoku

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

type Instance struct {
	ID     int
	Tag    string
	Config Config
}

type managed struct {
	proc              *Process
	config            Config
	fingerprint       string
	binary            string
	binaryFingerprint string
}

type Manager struct {
	mu      sync.Mutex
	procs   map[int]*managed
	lastErr map[int]string
}

var (
	managerOnce      sync.Once
	managerSingleton *Manager
)

func GetManager() *Manager {
	managerOnce.Do(func() {
		managerSingleton = &Manager{
			procs:   make(map[int]*managed),
			lastErr: make(map[int]string),
		}
	})
	return managerSingleton
}

func (m *Manager) Reconcile(ctx context.Context, desired []Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()

	binDir := config.GetBinFolderPath()
	if !IsInstalled(binDir) {
		for id, cur := range m.procs {
			if !m.stopTracked(id, cur, "binary unavailable") {
				continue
			}
			delete(m.procs, id)
			delete(m.lastErr, id)
			_ = os.Remove(configPath(binDir, id))
		}
		return
	}
	binary := GetBinaryPath(binDir)
	binaryFP := currentBinaryFingerprint(binary)

	want := make(map[int]Instance, len(desired))
	for _, inst := range desired {
		want[inst.ID] = inst
	}

	for id, cur := range m.procs {
		if _, ok := want[id]; ok {
			continue
		}
		if !m.stopTracked(id, cur, "inbound removed or disabled") {
			continue
		}
		delete(m.procs, id)
		delete(m.lastErr, id)
		_ = os.Remove(configPath(binDir, id))
	}

	for _, inst := range desired {
		if ctx != nil {
			select {
			case <-ctx.Done():
				m.recordError(inst.ID, ctx.Err(), "sudoku: reconcile cancelled before inbound %d (%s): %v", inst.ID, inst.Tag, ctx.Err())
				return
			default:
			}
		}

		fp := fingerprint(inst.Config)
		cur := m.procs[inst.ID]
		if cur != nil && cur.proc != nil && cur.proc.IsRunning() && cur.fingerprint == fp && cur.binaryFingerprint == binaryFP {
			delete(m.lastErr, inst.ID)
			continue
		}

		candidate, err := writeCandidateConfig(binDir, inst.ID, inst.Config)
		if err != nil {
			m.recordError(inst.ID, fmt.Errorf("write candidate config: %w", err), "sudoku: failed to prepare config for inbound %d (%s): %v", inst.ID, inst.Tag, err)
			continue
		}
		candidatePresent := true
		cleanupCandidate := func() {
			if candidatePresent {
				_ = os.Remove(candidate)
			}
		}

		// Validate the candidate before touching a healthy process. Start() validates
		// again after promotion as defence in depth.
		if err := validateConfig(binary, candidate); err != nil {
			cleanupCandidate()
			m.recordError(inst.ID, err, "sudoku: rejected config for inbound %d (%s): %v", inst.ID, inst.Tag, err)
			continue
		}

		finalPath := configPath(binDir, inst.ID)
		var oldConfig []byte
		if cur != nil {
			oldConfig, err = os.ReadFile(finalPath)
			if err != nil {
				if !os.IsNotExist(err) {
					cleanupCandidate()
					m.recordError(inst.ID, fmt.Errorf("read current config: %w", err), "sudoku: cannot snapshot current config for inbound %d (%s): %v", inst.ID, inst.Tag, err)
					continue
				}
				oldConfig, err = marshalConfig(cur.config)
				if err != nil {
					cleanupCandidate()
					m.recordError(inst.ID, fmt.Errorf("render rollback config: %w", err), "sudoku: cannot prepare rollback config for inbound %d (%s): %v", inst.ID, inst.Tag, err)
					continue
				}
			}
		}

		if cur != nil {
			if !m.stopTracked(inst.ID, cur, "restart") {
				cleanupCandidate()
				continue
			}
		}

		if err := promoteConfig(candidate, finalPath); err != nil {
			cleanupCandidate()
			promoteErr := fmt.Errorf("activate validated config: %w", err)
			if cur != nil {
				if restartErr := cur.proc.Start(binary); restartErr == nil {
					cur.binary = binary
					cur.binaryFingerprint = binaryFP
					m.recordError(inst.ID, promoteErr, "sudoku: failed to activate new config for inbound %d (%s); previous process restored: %v", inst.ID, inst.Tag, err)
					continue
				} else {
					promoteErr = fmt.Errorf("%w; restart previous process: %w", promoteErr, restartErr)
				}
			}
			m.recordError(inst.ID, promoteErr, "sudoku: failed to activate config for inbound %d (%s): %v", inst.ID, inst.Tag, promoteErr)
			continue
		}
		candidatePresent = false

		proc := NewProcess(finalPath, inst.Tag)
		if err := proc.Start(binary); err != nil {
			startErr := err
			if cur == nil {
				_ = os.Remove(finalPath)
				m.recordError(inst.ID, startErr, "sudoku: failed to start inbound %d (%s): %v", inst.ID, inst.Tag, startErr)
				continue
			}

			if rollbackErr := restoreConfig(finalPath, oldConfig); rollbackErr != nil {
				combined := fmt.Errorf("start new config: %w; restore previous config: %w", startErr, rollbackErr)
				m.recordError(inst.ID, combined, "sudoku: failed to start inbound %d (%s) and rollback config: %v", inst.ID, inst.Tag, combined)
				continue
			}
			if rollbackStartErr := cur.proc.Start(binary); rollbackStartErr != nil {
				combined := fmt.Errorf("start new config: %w; restart previous config: %w", startErr, rollbackStartErr)
				m.recordError(inst.ID, combined, "sudoku: failed to start inbound %d (%s) and restore previous process: %v", inst.ID, inst.Tag, combined)
				continue
			}
			cur.binary = binary
			cur.binaryFingerprint = binaryFP
			m.recordError(inst.ID, startErr, "sudoku: failed to start new config for inbound %d (%s); previous config restored: %v", inst.ID, inst.Tag, startErr)
			continue
		}

		m.procs[inst.ID] = &managed{
			proc:              proc,
			config:            inst.Config,
			fingerprint:       fp,
			binary:            binary,
			binaryFingerprint: binaryFP,
		}
		delete(m.lastErr, inst.ID)
		logger.Infof("sudoku: started inbound %d (%s)", inst.ID, inst.Tag)
	}
}

func (m *Manager) stopTracked(id int, cur *managed, reason string) bool {
	if cur == nil || cur.proc == nil {
		return true
	}
	if err := cur.proc.Stop(); err != nil {
		wrapped := fmt.Errorf("stop Sudoku process (%s): %w", reason, err)
		m.recordError(id, wrapped, "sudoku: failed to stop inbound %d during %s: %v", id, reason, err)
		return false
	}
	return true
}

func (m *Manager) recordError(id int, err error, format string, args ...any) {
	message := err.Error()
	if m.lastErr[id] == message {
		return
	}
	m.lastErr[id] = message
	logger.Warningf(format, args...)
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	binDir := config.GetBinFolderPath()
	for id, cur := range m.procs {
		if !m.stopTracked(id, cur, "manager shutdown") {
			continue
		}
		delete(m.procs, id)
		delete(m.lastErr, id)
		_ = os.Remove(configPath(binDir, id))
	}
}

func (m *Manager) Remove(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	binDir := config.GetBinFolderPath()
	if cur := m.procs[id]; cur != nil {
		if !m.stopTracked(id, cur, "inbound removal") {
			return
		}
		delete(m.procs, id)
	}
	delete(m.lastErr, id)
	_ = os.Remove(configPath(binDir, id))
	_ = os.Remove(keyPath(binDir, id))
	_ = os.Remove(clientRosterPath(binDir, id))
	_ = RemoveCredentialRotationState(binDir, id)
}

func currentBinaryFingerprint(path string) string {
	stat, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", stat.Size(), stat.ModTime().UnixNano())
}

func fingerprint(cfg Config) string {
	b, _ := json.Marshal(cfg)
	return string(b)
}

func marshalConfig(cfg Config) ([]byte, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writeCandidateConfig(binDir string, id int, cfg Config) (string, error) {
	dir := configDir(binDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	data, err := marshalConfig(cfg)
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, fmt.Sprintf(".sudoku-%d-*.json", id))
	if err != nil {
		return "", err
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o640); err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

func promoteConfig(candidate, finalPath string) error {
	if err := os.Rename(candidate, finalPath); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}

	// Windows cannot atomically replace an existing destination with Rename.
	// Move the previous config aside first so a failed promotion can restore it.
	dir := filepath.Dir(finalPath)
	backupFile, err := os.CreateTemp(dir, ".sudoku-config-backup-*")
	if err != nil {
		return err
	}
	backupPath := backupFile.Name()
	if err := backupFile.Close(); err != nil {
		_ = os.Remove(backupPath)
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return err
	}
	defer os.Remove(backupPath)

	hadPrevious := false
	if err := os.Rename(finalPath, backupPath); err == nil {
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.Rename(candidate, finalPath); err != nil {
		if hadPrevious {
			if restoreErr := os.Rename(backupPath, finalPath); restoreErr != nil {
				return fmt.Errorf("replace config: %w; restore previous config: %w", err, restoreErr)
			}
		}
		return err
	}
	return nil
}

func restoreConfig(path string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("previous config snapshot is empty")
	}
	return writeAtomicFile(path, data, 0o640)
}

// Stage complete contents before replacing credential state. Truncating the
// roster in place can lose the revocation baseline if a write is interrupted.
func writeAtomicFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".sudoku-state-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return promoteConfig(tmp, path)
}

func WriteMasterKey(binDir string, id int, key string) error {
	return writeAtomicFile(keyPath(binDir, id), []byte(key+"\n"), 0o600)
}

func ReadMasterKey(binDir string, id int) (string, error) {
	b, err := os.ReadFile(keyPath(binDir, id))
	if err != nil {
		return "", err
	}
	return stringTrim(b), nil
}

func stringTrim(b []byte) string {
	end := len(b)
	for end > 0 && (b[end-1] == '\n' || b[end-1] == '\r' || b[end-1] == ' ' || b[end-1] == '\t') {
		end--
	}
	start := 0
	for start < end && (b[start] == ' ' || b[start] == '\t') {
		start++
	}
	return string(b[start:end])
}
