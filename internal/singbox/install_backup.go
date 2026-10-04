package singbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SnapshotInstallation preserves the binary, companion libraries and config as
// one rollback unit. The caller must stop the process before invoking restore.
func SnapshotInstallation() (restore func() error, cleanup func(), err error) {
	dir := filepath.Dir(GetBinaryPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	backupDir, err := os.MkdirTemp(dir, ".sing-box-backup-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup = func() { _ = os.RemoveAll(backupDir) }
	type savedFile struct {
		path, backup string
		mode         os.FileMode
		exists       bool
	}
	var saved []savedFile
	paths := []string{GetBinaryPath(), filepath.Join(dir, "libcronet.so"), filepath.Join(dir, "libcronet.dll"), GetConfigPath()}
	for index, path := range paths {
		entry := savedFile{path: path, backup: filepath.Join(backupDir, fmt.Sprintf("%d", index))}
		info, statErr := os.Stat(path)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			cleanup()
			return nil, nil, statErr
		}
		if statErr == nil {
			entry.exists, entry.mode = true, info.Mode().Perm()
			if err := copyFile(path, entry.backup); err != nil {
				cleanup()
				return nil, nil, err
			}
			if err := os.Chmod(entry.backup, entry.mode); err != nil {
				cleanup()
				return nil, nil, err
			}
		}
		saved = append(saved, entry)
	}
	restore = func() error {
		var failures []error
		for _, entry := range saved {
			if !entry.exists {
				if err := os.Remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) {
					failures = append(failures, err)
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(entry.path), 0o755); err != nil {
				failures = append(failures, err)
				continue
			}
			file, err := os.CreateTemp(filepath.Dir(entry.path), ".sing-box-restore-*")
			if err != nil {
				failures = append(failures, err)
				continue
			}
			name := file.Name()
			_ = file.Close()
			if err := copyFile(entry.backup, name); err != nil {
				failures = append(failures, err)
			} else if err := os.Chmod(name, entry.mode); err != nil {
				failures = append(failures, err)
			} else if err := os.Rename(name, entry.path); err != nil {
				failures = append(failures, err)
			}
			_ = os.Remove(name)
		}
		if err := errors.Join(failures...); err != nil {
			return fmt.Errorf("restore failed; backup retained in %s: %w", backupDir, err)
		}
		return nil
	}
	return restore, cleanup, nil
}
