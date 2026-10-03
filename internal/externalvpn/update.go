package externalvpn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"net/http"
	"os/exec"
	"path/filepath"
)

const maxExternalVPNArchiveSize int64 = 256 << 20

var (
	externalVPNHTTPClient     = &http.Client{Timeout: 30 * time.Second}
	externalVPNDownloadClient = &http.Client{Timeout: 10 * time.Minute}
	externalVPNUpdateMu       sync.Mutex
	externalVersionPattern    = regexp.MustCompile(`(?i)\bv?(\d+(?:\.\d+)+(?:[-+][0-9a-z.-]+)?)\b`)
)

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

type githubRelease struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseSpec struct {
	protocol    model.Protocol
	api         string
	binaryName  string
	versionArgs []string
}

type BinaryStatus struct {
	Installed       bool   `json:"installed"`
	Version         string `json:"version"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Error           string `json:"error,omitempty"`
}

type UpdateStatus struct {
	Pingtunnel  BinaryStatus `json:"pingtunnel"`
	TrustTunnel BinaryStatus `json:"trusttunnel"`
}

func releaseSpecFor(protocol model.Protocol) (releaseSpec, error) {
	switch protocol {
	case model.Pingtunnel:
		return releaseSpec{
			protocol:    protocol,
			api:         "https://api.github.com/repos/esrrhs/pingtunnel/releases/latest",
			binaryName:  "pingtunnel",
			versionArgs: []string{"-version"},
		}, nil
	case model.TrustTunnel:
		return releaseSpec{
			protocol:    protocol,
			api:         "https://api.github.com/repos/TrustTunnel/TrustTunnel/releases/latest",
			binaryName:  "trusttunnel_endpoint",
			versionArgs: []string{"--version"},
		}, nil
	default:
		return releaseSpec{}, fmt.Errorf("unsupported external VPN protocol %q", protocol)
	}
}

func GetUpdateStatus(ctx context.Context) UpdateStatus {
	var status UpdateStatus
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		status.Pingtunnel = binaryStatus(ctx, model.Pingtunnel)
	}()
	go func() {
		defer wg.Done()
		status.TrustTunnel = binaryStatus(ctx, model.TrustTunnel)
	}()
	wg.Wait()
	return status
}

func binaryStatus(ctx context.Context, protocol model.Protocol) BinaryStatus {
	spec, err := releaseSpecFor(protocol)
	if err != nil {
		return BinaryStatus{Error: err.Error()}
	}
	path := binary(protocol)
	info, statErr := os.Stat(path)
	status := BinaryStatus{Installed: statErr == nil && !info.IsDir()}
	if status.Installed {
		if version, versionErr := binaryVersion(ctx, path, spec.versionArgs); versionErr == nil {
			status.Version = version
		}
	}
	release, err := fetchLatestRelease(ctx, spec)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.LatestVersion = normalizeReleaseVersion(release.TagName)
	if status.Installed && status.LatestVersion != "" {
		status.UpdateAvailable = status.Version == "" || normalizeReleaseVersion(status.Version) != status.LatestVersion
	}
	return status
}

func binaryVersion(ctx context.Context, path string, args []string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandCtx, path, args...).CombinedOutput()
	version := parseExternalVersion(string(output))
	if version != "" {
		return version, nil
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("version was not reported by %s", filepath.Base(path))
}

func parseExternalVersion(value string) string {
	match := externalVersionPattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return normalizeReleaseVersion(match[1])
}

func normalizeReleaseVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	value = strings.TrimPrefix(value, "V")
	return value
}

func fetchLatestRelease(ctx context.Context, spec releaseSpec) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.api, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "3x-ui")
	resp, err := externalVPNHTTPClient.Do(req)
	if err != nil {
		return githubRelease{}, fmt.Errorf("fetch %s release: %w", spec.protocol, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("%s release API returned %s", spec.protocol, resp.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode %s release: %w", spec.protocol, err)
	}
	if normalizeReleaseVersion(release.TagName) == "" {
		return githubRelease{}, fmt.Errorf("%s release API returned an empty tag", spec.protocol)
	}
	return release, nil
}

func Update(ctx context.Context, protocol model.Protocol) error {
	if protocol == model.OpenFlux {
		return InstallOpenFlux(ctx)
	}
	if protocol == model.FPTN {
		return InstallFPTNClient(ctx)
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("%s updater is supported only on Linux", protocol)
	}
	spec, err := releaseSpecFor(protocol)
	if err != nil {
		return err
	}

	// Updating replaces files shared by all inbounds of a protocol. Serialize
	// installs so two clicks (or two browser sessions) cannot race on staging,
	// process restarts, or the final binary replacement.
	externalVPNUpdateMu.Lock()
	defer externalVPNUpdateMu.Unlock()

	release, err := fetchLatestRelease(ctx, spec)
	if err != nil {
		return err
	}
	asset, err := pickReleaseAsset(spec, release, runtime.GOARCH)
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "3x-ui-externalvpn-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, filepath.Base(asset.Name))
	if err := downloadReleaseAsset(ctx, asset.URL, archivePath); err != nil {
		return err
	}
	if err := verifyReleaseDigest(archivePath, asset.Digest); err != nil {
		return err
	}
	extracted, err := extractReleaseBinary(archivePath, tmpDir, spec.binaryName)
	if err != nil {
		return err
	}

	binDir := config.GetBinFolderPath()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	staged, err := os.CreateTemp(binDir, "."+spec.binaryName+"-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)

	source, err := os.Open(extracted)
	if err != nil {
		_ = staged.Close()
		return err
	}
	_, copyErr := io.Copy(staged, source)
	closeSourceErr := source.Close()
	syncErr := staged.Sync()
	closeStagedErr := staged.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeSourceErr != nil {
		return closeSourceErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeStagedErr != nil {
		return closeStagedErr
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil {
		return err
	}

	// Never replace a working binary with an archive member that cannot execute
	// on this host or does not identify itself as the release we selected.
	stagedVersion, err := binaryVersion(ctx, stagedPath, spec.versionArgs)
	if err != nil {
		return fmt.Errorf("validate %s binary: %w", protocol, err)
	}
	expectedVersion := normalizeReleaseVersion(release.TagName)
	if expectedVersion != "" && normalizeReleaseVersion(stagedVersion) != expectedVersion {
		return fmt.Errorf("validate %s binary: expected version %s, got %s", protocol, expectedVersion, stagedVersion)
	}

	target := filepath.Join(binDir, spec.binaryName)
	if err := os.Rename(stagedPath, target); err != nil {
		return fmt.Errorf("install %s: %w", protocol, err)
	}
	// Linux keeps a running executable mapped after an atomic rename. Stop only
	// after the new binary is in place; the normal reconciliation job will then
	// restart enabled inbounds with the newly installed executable.
	GetManager().StopProtocol(protocol)
	return nil
}

func pickReleaseAsset(spec releaseSpec, release githubRelease, goarch string) (releaseAsset, error) {
	var expected string
	version := normalizeReleaseVersion(release.TagName)
	switch spec.protocol {
	case model.Pingtunnel:
		switch goarch {
		case "amd64", "arm64":
			expected = fmt.Sprintf("pingtunnel_linux_%s.zip", goarch)
		default:
			return releaseAsset{}, fmt.Errorf("Pingtunnel is not published for linux/%s", goarch)
		}
	case model.TrustTunnel:
		arch := ""
		switch goarch {
		case "amd64":
			arch = "x86_64"
		case "arm64":
			arch = "aarch64"
		default:
			return releaseAsset{}, fmt.Errorf("TrustTunnel is not published for linux/%s", goarch)
		}
		expected = fmt.Sprintf("trusttunnel-v%s-linux-%s.tar.gz", version, arch)
	default:
		return releaseAsset{}, fmt.Errorf("unsupported external VPN protocol %q", spec.protocol)
	}
	for _, asset := range release.Assets {
		if asset.Name == expected && strings.TrimSpace(asset.URL) != "" {
			return asset, nil
		}
	}
	return releaseAsset{}, fmt.Errorf("release %s has no asset %s", release.TagName, expected)
}

func downloadReleaseAsset(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "3x-ui")
	resp, err := externalVPNDownloadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("external VPN download returned %s", resp.Status)
	}
	if resp.ContentLength > maxExternalVPNArchiveSize {
		return fmt.Errorf("external VPN release archive exceeds %d MiB", maxExternalVPNArchiveSize>>20)
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(out, io.LimitReader(resp.Body, maxExternalVPNArchiveSize+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxExternalVPNArchiveSize {
		return fmt.Errorf("external VPN release archive exceeds %d MiB", maxExternalVPNArchiveSize>>20)
	}
	return nil
}

func verifyReleaseDigest(path, digest string) error {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil
	}
	algorithm, expected, ok := strings.Cut(digest, ":")
	if !ok || !strings.EqualFold(strings.TrimSpace(algorithm), "sha256") {
		return fmt.Errorf("unsupported release digest %q", digest)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
		return fmt.Errorf("release digest mismatch")
	}
	return nil
}

func extractReleaseBinary(archivePath, dir, binaryName string) (string, error) {
	lower := strings.ToLower(archivePath)
	if strings.HasSuffix(lower, ".zip") {
		reader, err := zip.OpenReader(archivePath)
		if err != nil {
			return "", err
		}
		defer reader.Close()
		for _, file := range reader.File {
			if filepath.Base(file.Name) != binaryName || file.FileInfo().IsDir() {
				continue
			}
			source, err := file.Open()
			if err != nil {
				return "", err
			}
			path := filepath.Join(dir, binaryName+".extracted")
			err = copyExtractedBinary(path, source)
			source.Close()
			if err != nil {
				return "", err
			}
			return path, nil
		}
		return "", fmt.Errorf("%s binary not found in release archive", binaryName)
	}
	if !strings.HasSuffix(lower, ".tar.gz") && !strings.HasSuffix(lower, ".tgz") {
		return "", fmt.Errorf("unsupported release archive %s", filepath.Base(archivePath))
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if filepath.Base(header.Name) != binaryName || header.Typeflag != tar.TypeReg {
			continue
		}
		path := filepath.Join(dir, binaryName+".extracted")
		if err := copyExtractedBinary(path, tarReader); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("%s binary not found in release archive", binaryName)
}

func copyExtractedBinary(path string, source io.Reader) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(out, io.LimitReader(source, maxExternalVPNArchiveSize+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxExternalVPNArchiveSize {
		return fmt.Errorf("external VPN binary exceeds %d MiB", maxExternalVPNArchiveSize>>20)
	}
	return nil
}
