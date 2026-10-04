package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
)

// Check a candidate before replacing the file used by the running core. A
// failed check leaves both the existing config and the process untouched.
func (s *SingBoxService) writeConfigCandidate(ctx context.Context) error {
	cfg, err := s.GetConfig()
	if err != nil {
		return err
	}
	data, err := cfg.Marshal()
	if err != nil {
		return err
	}
	path := singbox.GetConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sing-box-config-*.json")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := singbox.NewTestProcess(name).Validate(ctx); err != nil {
		return fmt.Errorf("validate candidate sing-box config: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
