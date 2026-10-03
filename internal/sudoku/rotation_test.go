package sudoku

import "testing"

func TestCredentialRotationMarkerLifecycle(t *testing.T) {
	binDir := t.TempDir()
	const inboundID = 17

	pending, err := CredentialRotationPending(binDir, inboundID)
	if err != nil {
		t.Fatal(err)
	}
	if pending {
		t.Fatal("fresh inbound unexpectedly has a pending rotation")
	}

	if err := BeginCredentialRotation(binDir, inboundID); err != nil {
		t.Fatal(err)
	}
	pending, err = CredentialRotationPending(binDir, inboundID)
	if err != nil {
		t.Fatal(err)
	}
	if !pending {
		t.Fatal("rotation marker was not persisted")
	}

	// Beginning again is intentionally idempotent: retries must keep the
	// recovery marker present until the whole credential update commits.
	if err := BeginCredentialRotation(binDir, inboundID); err != nil {
		t.Fatal(err)
	}
	if err := CompleteCredentialRotation(binDir, inboundID); err != nil {
		t.Fatal(err)
	}
	pending, err = CredentialRotationPending(binDir, inboundID)
	if err != nil {
		t.Fatal(err)
	}
	if pending {
		t.Fatal("rotation marker survived successful completion")
	}

	// Completion is also idempotent, which keeps cleanup safe after partial
	// removals or repeated reconcile attempts.
	if err := CompleteCredentialRotation(binDir, inboundID); err != nil {
		t.Fatal(err)
	}
}
