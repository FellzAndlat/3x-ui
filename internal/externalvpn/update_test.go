package externalvpn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func TestParseExternalVersion(t *testing.T) {
	for input, want := range map[string]string{
		"pingtunnel 2.10 (linux/amd64)":     "2.10",
		"trusttunnel_endpoint v1.1.0":       "1.1.0",
		"TrustTunnel endpoint 1.2.3-beta.1": "1.2.3-beta.1",
	} {
		if got := parseExternalVersion(input); got != want {
			t.Fatalf("parseExternalVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPickReleaseAsset(t *testing.T) {
	pingSpec, err := releaseSpecFor(model.Pingtunnel)
	if err != nil {
		t.Fatal(err)
	}
	pingRelease := githubRelease{TagName: "2.10", Assets: []releaseAsset{{Name: "pingtunnel_linux_amd64.zip", URL: "https://example.invalid/ping.zip"}}}
	asset, err := pickReleaseAsset(pingSpec, pingRelease, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "pingtunnel_linux_amd64.zip" {
		t.Fatalf("Pingtunnel asset = %q", asset.Name)
	}

	trustSpec, err := releaseSpecFor(model.TrustTunnel)
	if err != nil {
		t.Fatal(err)
	}
	trustRelease := githubRelease{TagName: "v1.1.0", Assets: []releaseAsset{{Name: "trusttunnel-v1.1.0-linux-aarch64.tar.gz", URL: "https://example.invalid/trust.tar.gz"}}}
	asset, err = pickReleaseAsset(trustSpec, trustRelease, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "trusttunnel-v1.1.0-linux-aarch64.tar.gz" {
		t.Fatalf("TrustTunnel asset = %q", asset.Name)
	}
}

func TestVerifyReleaseDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "asset")
	if err := os.WriteFile(path, []byte("3x-ui"), 0o600); err != nil {
		t.Fatal(err)
	}
	const digest = "sha256:1343462835cbb1f7631c393c1090169aee14f90129e0922c7ec1d58fd40b0647"
	if err := verifyReleaseDigest(path, digest); err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseDigest(path, "sha256:0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("digest mismatch was accepted")
	}
}
