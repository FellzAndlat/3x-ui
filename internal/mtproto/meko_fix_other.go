//go:build !linux

package mtproto

import "fmt"

func applyMekoFix(_ int, _ int, cfg MekoFixConfig) error {
	if cfg.Enabled {
		return fmt.Errorf("MEKO SYN fix is supported only on Linux")
	}
	return nil
}

func removeMekoFix(_ int) {}
