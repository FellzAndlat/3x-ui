package amneziawgnet

import (
	"errors"
	"syscall"
	"testing"

	"github.com/amnezia-vpn/amneziawg-go/v3/device"
)

// The real handshake tests require the host to allow the device's UDP bind.
// Sandboxed runners may forbid it even when ordinary loopback sockets work.
func upTestClient(t *testing.T, client *device.Device) {
	t.Helper()
	if err := client.Up(); err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("AmneziaWG UDP bind is not permitted in this environment: %v", err)
		}
		t.Fatalf("client Up: %v", err)
	}
}
