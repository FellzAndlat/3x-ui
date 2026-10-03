package naiveproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

const (
	releasesAPI       = "https://api.github.com/repos/klzgrad/forwardproxy/releases/latest"
	releaseAssetName  = "caddy-forwardproxy-naive.tar.xz"
	reconcileInterval = 3 * time.Second
)

var managerMu sync.Mutex

type Status struct {
	Installed       bool   `json:"installed"`
	Version         string `json:"version"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Binary          string `json:"binary"`
	Running         bool   `json:"running"`
}

type User struct {
	Username string
	Password string
}

type Inbound struct {
	Tag             string
	Listen          string
	Port            int
	CertificatePath string
	KeyPath         string
	Users           []User
}

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type inboundSettings struct {
	Clients []model.Client `json:"clients"`
	TLS     struct {
		CertificatePath string `json:"certificatePath"`
		KeyPath         string `json:"keyPath"`
	} `json:"tls"`
}

func GetStatus(ctx context.Context) (Status, error) {
	binary := BinaryPath()
	_, statErr := os.Stat(binary)
	status := Status{
		Installed: statErr == nil,
		Version:   installedVersion(binary),
		Binary:    binary,
		Running:   processRunning(),
	}

	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		// Network failure must not make an installed component look absent.
		return status, nil
	}
	status.LatestVersion = strings.TrimSpace(rel.TagName)
	if status.Installed && status.LatestVersion != "" {
		status.UpdateAvailable = normalizeVersion(status.Version) != normalizeVersion(status.LatestVersion)
	}
	return status, nil
}

// Update installs the official Caddy build carrying klzgrad/forwardproxy@naive.
// The official upstream archive currently targets Linux amd64; other platforms
// must use sing-box's native Naive implementation instead of this sidecar.
func Update(ctx context.Context) (Status, error) {
	managerMu.Lock()
	defer managerMu.Unlock()

	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return Status{}, fmt.Errorf("standalone NaiveProxy server is supported on Linux amd64 only")
	}
	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return Status{}, err
	}
	asset, err := pickAsset(rel.Assets)
	if err != nil {
		return Status{}, err
	}

	tmpDir, err := os.MkdirTemp("", "3x-ui-caddy-naive-*")
	if err != nil {
		return Status{}, err
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, asset.Name)
	if err := downloadAndVerify(ctx, asset, archivePath); err != nil {
		return Status{}, err
	}
	if err := extractTarXZ(ctx, archivePath, tmpDir); err != nil {
		return Status{}, err
	}
	extracted, err := findExtractedBinary(tmpDir)
	if err != nil {
		return Status{}, err
	}

	_ = stopLocked()
	binDir := config.GetBinFolderPath()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return Status{}, err
	}
	staged := filepath.Join(binDir, ".caddy-naive.new")
	if err := copyFile(extracted, staged); err != nil {
		return Status{}, err
	}
	if err := os.Chmod(staged, 0o755); err != nil {
		_ = os.Remove(staged)
		return Status{}, err
	}
	if err := os.Rename(staged, BinaryPath()); err != nil {
		_ = os.Remove(staged)
		return Status{}, err
	}
	if err := os.WriteFile(versionPath(), []byte(strings.TrimSpace(rel.TagName)+"\n"), 0o644); err != nil {
		return Status{}, err
	}

	// Do not recurse through the manager lock. Ensure/reconciler starts the
	// newly installed binary after Update returns.
	status, err := statusWithoutRelease()
	if err != nil {
		return Status{}, err
	}
	status.LatestVersion = strings.TrimSpace(rel.TagName)
	status.UpdateAvailable = false
	return status, nil
}

// Sync writes and validates a complete Caddyfile, then atomically restarts the
// sidecar only when its effective configuration changed.
func Sync(ctx context.Context, inbounds []Inbound) error {
	managerMu.Lock()
	defer managerMu.Unlock()

	if len(inbounds) == 0 {
		return stopLocked()
	}
	if _, err := os.Stat(BinaryPath()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("NaiveProxy server is not installed; install Caddy-Naive from System Updates")
		}
		return err
	}

	content, err := RenderConfig(inbounds)
	if err != nil {
		return err
	}
	old, _ := os.ReadFile(configPath())
	if string(old) == content && processRunning() {
		return nil
	}

	if err := os.MkdirAll(config.GetBinFolderPath(), 0o755); err != nil {
		return err
	}
	staged := configPath() + ".new"
	if err := os.WriteFile(staged, []byte(content), 0o600); err != nil {
		return err
	}
	if err := validateConfig(ctx, staged); err != nil {
		_ = os.Remove(staged)
		return err
	}
	if err := os.Rename(staged, configPath()); err != nil {
		_ = os.Remove(staged)
		return err
	}
	if err := stopLocked(); err != nil {
		return err
	}
	return startLocked()
}

func RenderConfig(inbounds []Inbound) (string, error) {
	if len(inbounds) == 0 {
		return "", nil
	}
	items := append([]Inbound(nil), inbounds...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Port == items[j].Port {
			return items[i].Tag < items[j].Tag
		}
		return items[i].Port < items[j].Port
	})
	ports := make(map[int]string, len(items))
	var b strings.Builder
	b.WriteString("{\n\tadmin off\n\torder forward_proxy before file_server\n}\n\n")

	for _, inbound := range items {
		if inbound.Port < 1 || inbound.Port > 65535 {
			return "", fmt.Errorf("Naive inbound %q has invalid port %d", inbound.Tag, inbound.Port)
		}
		if previous, exists := ports[inbound.Port]; exists {
			return "", fmt.Errorf("Naive inbounds %q and %q use the same port %d", previous, inbound.Tag, inbound.Port)
		}
		ports[inbound.Port] = inbound.Tag
		if inbound.CertificatePath == "" || inbound.KeyPath == "" {
			return "", fmt.Errorf("Naive inbound %q requires certificatePath and keyPath", inbound.Tag)
		}
		users := make([]User, 0, len(inbound.Users))
		for _, user := range inbound.Users {
			username := strings.TrimSpace(user.Username)
			if username == "" || user.Password == "" {
				continue
			}
			users = append(users, User{Username: username, Password: user.Password})
		}
		if len(users) == 0 {
			return "", fmt.Errorf("Naive inbound %q has no enabled users", inbound.Tag)
		}

		fmt.Fprintf(&b, ":%d {\n", inbound.Port)
		listen := strings.TrimSpace(inbound.Listen)
		if listen != "" && listen != "0.0.0.0" && listen != "::" && listen != "[::]" {
			if strings.HasPrefix(listen, "@") || strings.HasPrefix(listen, "/") {
				return "", fmt.Errorf("Naive inbound %q cannot bind to a unix socket", inbound.Tag)
			}
			if host, _, err := net.SplitHostPort(listen); err == nil {
				listen = host
			}
			listen = strings.Trim(listen, "[]")
			fmt.Fprintf(&b, "\tbind %s\n", caddyQuote(listen))
		}
		fmt.Fprintf(&b, "\ttls %s %s\n", caddyQuote(inbound.CertificatePath), caddyQuote(inbound.KeyPath))
		b.WriteString("\tforward_proxy {\n")
		for _, user := range users {
			fmt.Fprintf(&b, "\t\tbasic_auth %s %s\n", caddyQuote(user.Username), caddyQuote(user.Password))
		}
		b.WriteString("\t\thide_ip\n\t\thide_via\n\t\tprobe_resistance\n\t}\n}\n\n")
	}
	return b.String(), nil
}

func caddyQuote(value string) string { return strconv.Quote(value) }

func Stop() error {
	managerMu.Lock()
	defer managerMu.Unlock()
	return stopLocked()
}

func startLocked() error {
	if processRunning() {
		return nil
	}
	logFile, err := os.OpenFile(logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(context.Background(), BinaryPath(), "run", "--config", configPath(), "--adapter", "caddyfile")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start Caddy-Naive: %w", err)
	}
	_ = logFile.Close()
	if err := os.WriteFile(pidPath(), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	go func() {
		_ = cmd.Wait()
		if data, err := os.ReadFile(pidPath()); err == nil && strings.TrimSpace(string(data)) == strconv.Itoa(cmd.Process.Pid) {
			_ = os.Remove(pidPath())
		}
	}()
	time.Sleep(250 * time.Millisecond)
	if !processRunning() {
		return fmt.Errorf("Caddy-Naive exited immediately; see %s", logPath())
	}
	return nil
}

func stopLocked() error {
	pid, ok := readPID()
	if !ok {
		_ = os.Remove(pidPath())
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(pidPath())
		return nil
	}
	if runtime.GOOS == "linux" && !pidBelongsToBinary(pid) {
		_ = os.Remove(pidPath())
		return nil
	}
	_ = proc.Signal(syscall.SIGTERM)
	for i := 0; i < 20; i++ {
		if !pidAlive(pid) {
			_ = os.Remove(pidPath())
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = proc.Kill()
	_ = os.Remove(pidPath())
	return nil
}

func processRunning() bool {
	pid, ok := readPID()
	if !ok || !pidAlive(pid) {
		return false
	}
	return runtime.GOOS != "linux" || pidBelongsToBinary(pid)
}

func readPID() (int, bool) {
	data, err := os.ReadFile(pidPath())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid, err == nil && pid > 1
}

func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func pidBelongsToBinary(pid int) bool {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return false
	}
	want, err := filepath.EvalSymlinks(BinaryPath())
	if err != nil {
		want = BinaryPath()
	}
	got, err := filepath.EvalSymlinks(exe)
	if err != nil {
		got = exe
	}
	return got == want
}

func validateConfig(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, BinaryPath(), "validate", "--config", path, "--adapter", "caddyfile")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("validate Caddy-Naive config: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func BinaryPath() string  { return filepath.Join(config.GetBinFolderPath(), "caddy-naive") }
func versionPath() string { return filepath.Join(config.GetBinFolderPath(), "caddy-naive.version") }
func configPath() string  { return filepath.Join(config.GetBinFolderPath(), "caddy-naive.Caddyfile") }
func pidPath() string     { return filepath.Join(config.GetBinFolderPath(), "caddy-naive.pid") }
func logPath() string     { return filepath.Join(config.GetBinFolderPath(), "caddy-naive.log") }
func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

func statusWithoutRelease() (Status, error) {
	binary := BinaryPath()
	_, err := os.Stat(binary)
	return Status{Installed: err == nil, Version: installedVersion(binary), Binary: binary, Running: processRunning()}, nil
}

func installedVersion(binary string) string {
	if data, err := os.ReadFile(versionPath()); err == nil {
		if value := strings.TrimSpace(string(data)); value != "" {
			return value
		}
	}
	if _, err := os.Stat(binary); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, "version").CombinedOutput()
	if err != nil && len(output) == 0 {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func fetchLatestRelease(ctx context.Context) (release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "3x-ui")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return release{}, fmt.Errorf("fetch Caddy-Naive release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("Caddy-Naive release API returned %s", resp.Status)
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return release{}, err
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return release{}, fmt.Errorf("Caddy-Naive release API returned an empty tag")
	}
	return rel, nil
}

func pickAsset(assets []releaseAsset) (releaseAsset, error) {
	for _, asset := range assets {
		if asset.Name == releaseAssetName && asset.URL != "" {
			return asset, nil
		}
	}
	return releaseAsset{}, fmt.Errorf("official release does not contain %s", releaseAssetName)
}

func downloadAndVerify(ctx context.Context, asset releaseAsset, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "3x-ui")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Caddy-Naive download returned %s", resp.Status)
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, hash), resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if strings.HasPrefix(strings.ToLower(asset.Digest), "sha256:") {
		expected := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(asset.Digest), "sha256:"))
		actual := hex.EncodeToString(hash.Sum(nil))
		if expected != "" && actual != expected {
			return fmt.Errorf("Caddy-Naive archive checksum mismatch")
		}
	}
	return nil
}

func extractTarXZ(ctx context.Context, archivePath, dst string) error {
	if _, err := exec.LookPath("tar"); err != nil {
		return fmt.Errorf("tar with xz support is required to install Caddy-Naive: %w", err)
	}
	cmd := exec.CommandContext(ctx, "tar", "-xJf", archivePath, "-C", dst)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract Caddy-Naive: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func findExtractedBinary(root string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		name := strings.ToLower(filepath.Base(path))
		if name == "caddy" || name == "caddy-forwardproxy-naive" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && found == "" {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("Caddy binary not found in %s", releaseAssetName)
	}
	return found, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
