package sudoku

import (
	"context"
	"net"
	"os"
	"testing"
)

// Set SUDOKU_TEST_BINARY to an upstream sudoku-tunnel build to verify the CLI
// and server configuration contract without downloading executables in tests.
func TestSudokuBinaryIntegration(t *testing.T) {
	binary := os.Getenv("SUDOKU_TEST_BINARY")
	if binary == "" {
		t.Skip("set SUDOKU_TEST_BINARY to run against upstream")
	}
	public, private, err := GenerateMasterKey(context.Background(), binary)
	if err != nil {
		t.Fatal(err)
	}
	split, err := GenerateSplitKey(context.Background(), binary, private)
	if err != nil || !ValidPrivateKey(split) {
		t.Fatalf("split key generation: %v", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Mode: "server", Transport: "tcp", LocalPort: port, Key: public,
		AEAD: "chacha20-poly1305", SuspiciousAction: "silent", PaddingMin: 5, PaddingMax: 15,
		ASCII: "prefer_entropy", EnablePureDownlink: true, Multiplex: "off", HTTPMask: HTTPMaskConfig{Mode: "auto", Multiplex: "off"},
	}
	path, err := writeCandidateConfig(t.TempDir(), 1, cfg)
	if err != nil {
		t.Fatal(err)
	}
	process := NewProcess(path, "upstream-contract")
	t.Cleanup(func() {
		if err := process.Stop(); err != nil {
			t.Error(err)
		}
	})
	if err := process.Start(binary); err != nil {
		t.Fatal(err)
	}
	if !process.IsRunning() {
		t.Fatal("validated server exited")
	}
}
