package sudoku

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func credentialRotationPath(binDir string, id int) string {
	return filepath.Join(configDir(binDir), fmt.Sprintf("sudoku-%d.rotate", id))
}

// CredentialRotationPending reports whether a previous master-key rotation
// started but did not finish. A pending marker makes credential reconciliation
// rotate again instead of trusting a possibly mismatched public/private pair.
func CredentialRotationPending(binDir string, id int) (bool, error) {
	_, err := os.Stat(credentialRotationPath(binDir, id))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// BeginCredentialRotation persists intent before either the database settings
// or the private-key file is changed. If the process crashes or a later write
// fails, the marker survives and the next reconcile safely rotates again.
func BeginCredentialRotation(binDir string, id int) error {
	return writeAtomicFile(credentialRotationPath(binDir, id), []byte("pending\n"), 0o600)
}

func CompleteCredentialRotation(binDir string, id int) error {
	err := os.Remove(credentialRotationPath(binDir, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func RemoveCredentialRotationState(binDir string, id int) error {
	return CompleteCredentialRotation(binDir, id)
}
