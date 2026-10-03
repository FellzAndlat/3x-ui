package sudoku

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenerateKeysFromUpstreamLogOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	public, private, split := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 128)
	binary := filepath.Join(t.TempDir(), "sudoku")
	script := "#!/bin/sh\nif [ \"$2\" = \"-more\" ]; then\n echo '12:00:00 info [CLI] Split Private Key: " + split + "'\nelse\n echo '12:00:00 info [CLI] Master Public Key:  " + public + "'\n echo '12:00:00 info [CLI] Master Private Key: " + private + "'\nfi\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := GenerateMasterKey(context.Background(), binary)
	if err != nil {
		t.Fatal(err)
	}
	if pub != public || priv != private {
		t.Fatal("incorrect master keys")
	}
	got, err := GenerateSplitKey(context.Background(), binary, priv)
	if err != nil {
		t.Fatal(err)
	}
	if got != split {
		t.Fatal("incorrect split key")
	}
}
