package externalvpn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func containerName(id int) string {
	// Distinguish panels installed in different directories on the same host.
	sum := sha256.Sum256([]byte(directory()))
	return fmt.Sprintf("3x-ui-fptn-%s-%d", hex.EncodeToString(sum[:4]), id)
}

func (m *Manager) additionalCommand(inst Instance, metricsAddr string) (*exec.Cmd, context.CancelFunc, error) {
	folder := filepath.Join(directory(), strconv.Itoa(inst.ID))
	if err := os.MkdirAll(folder, 0700); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	fail := func(err error) (*exec.Cmd, context.CancelFunc, error) { cancel(); return nil, nil, err }
	if inst.Protocol == model.OpenFlux {
		var client Client
		for _, c := range inst.Settings.Clients {
			if c.Enable {
				client = c
				break
			}
		}
		secret := filepath.Join(folder, "secret")
		if err := writePrivate(secret, []byte(client.Password)); err != nil {
			return fail(err)
		}
		socket := filepath.Join(folder, "ipc.sock")
		// The file parser does not support quoted or escaped values. Validate those
		// values before writing and use generated absolute paths without INI comments.
		for _, value := range []string{secret, socket} {
			if strings.ContainsAny(value, "\r\n#;") {
				return fail(fmt.Errorf("unsupported runtime directory"))
			}
		}
		var conf strings.Builder
		fmt.Fprintf(&conf, "[Interface]\nRole = exit\nMode = l4\nCodec = batched\nEncryptionKeyFile = %s\nSessionContext = %s\nIPCSocket = %s\nCookieStore = %s\n", secret, inst.Settings.Context, socket, filepath.Join(folder, "cookies.json"))
		for _, t := range inst.Settings.Transports {
			fmt.Fprintf(&conf, "\n[Transport \"%s\"]\nType = %s\nPriority = %d\n", t.Type, t.Type, t.Priority)
			if t.Type == "direct" {
				fmt.Fprintf(&conf, "Listen = %s\n", inst.Bind())
			} else {
				fmt.Fprintf(&conf, "URL = %s\n", t.URL)
			}
		}
		path := filepath.Join(folder, "openflux.conf")
		if err := writePrivate(path, []byte(conf.String())); err != nil {
			return fail(err)
		}
		return exec.CommandContext(ctx, binary(inst.Protocol), "--config", path, "--negotiate"), cancel, nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return fail(fmt.Errorf("FPTN requires Docker for isolated networking: %w", err))
	}
	cert, key, err := fptnCertificateFiles(inst, folder)
	if err != nil {
		return fail(err)
	}
	// Copy custom files into the private mount; never mount their parent directory.
	for src, name := range map[string]string{cert: "server.crt", key: "server.key"} {
		data, err := os.ReadFile(src)
		if err != nil {
			return fail(err)
		}
		if err := writePrivate(filepath.Join(folder, name), data); err != nil {
			return fail(err)
		}
	}
	var users strings.Builder
	for _, c := range inst.Settings.Clients {
		if c.Enable {
			sum := sha256.Sum256([]byte(c.Password))
			bandwidth := inst.Settings.Bandwidth
			// Upstream treats zero as a zero-rate bucket, not unlimited.
			// Use its largest safe signed-int rate for the panel's unlimited preset.
			if bandwidth == 0 {
				bandwidth = 2000
			}
			fmt.Fprintf(&users, "%s %x %d\n", FPTNUsername(c.Email), sum, bandwidth)
		}
	}
	if err := writePrivate(filepath.Join(folder, "users.list"), []byte(users.String())); err != nil {
		return fail(err)
	}
	// The official image starts HAProxy, DNS and FPTN together. All sysctls and
	// firewall policy changes made by FPTN remain in Docker's network namespace.
	args := []string{"run", "--rm", "--name", containerName(inst.ID), "--label", "app=3x-ui-fptn", "--cap-add", "NET_ADMIN", "--cap-add", "NET_RAW", "--device", "/dev/net/tun:/dev/net/tun", "--publish", inst.Bind() + ":443/tcp", "--publish", metricsAddr + ":443/tcp", "--mount", "type=bind,src=" + folder + ",dst=/etc/fptn", "--ulimit", "nofile=65536:65536"}
	env := map[string]string{
		"ENABLE_DETECT_PROBING": strconv.FormatBool(inst.Settings.DetectProbing), "ALLOWED_SNI_LIST": inst.Settings.AllowedSNI,
		"ENABLE_DOMAIN_BLACKLIST_FILTER": "false", "DOMAIN_BLACKLIST_URLS": "", "ENABLE_ADS_FILTER": strconv.FormatBool(inst.Settings.AdsFilter), "ADS_BLOCKLIST_URLS": "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
		"ENABLE_TORRENT_FILTER": strconv.FormatBool(inst.Settings.TorrentFilter), "ENABLE_SPAM_FILTER": strconv.FormatBool(inst.Settings.SpamFilter),
		"PROMETHEUS_SECRET_ACCESS_KEY": inst.Settings.MetricsKey, "USE_REMOTE_SERVER_AUTH": "false", "MAX_ACTIVE_SESSIONS_PER_USER": strconv.Itoa(inst.Settings.MaxSessions),
		"REMOTE_SERVER_AUTH_HOST": "127.0.0.1", "REMOTE_SERVER_AUTH_PORT": "8080", "SERVER_EXTERNAL_IPS": "",
		"MTU_SIZE": strconv.Itoa(inst.Settings.MTU), "USING_DNS_SERVER": "unbound", "DNS_IPV6_ENABLE": "false", "DNS_IPV4_PRIMARY": "1.1.1.1", "DNS_IPV4_SECONDARY": "9.9.9.9", "DATA_DIR": "/etc/fptn/data",
	}
	var envFile strings.Builder
	for name, value := range env {
		fmt.Fprintf(&envFile, "%s=%s\n", name, value)
	}
	path := filepath.Join(folder, "docker.env")
	if err := writePrivate(path, []byte(envFile.String())); err != nil {
		return fail(err)
	}
	args = append(args, "--env-file", path, inst.Settings.Image)
	// Remove only this panel's deterministic container after a previous crash.
	cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", containerName(inst.ID)).Run()
	done()
	return exec.CommandContext(ctx, "docker", args...), cancel, nil
}

func cleanupAdditional(id int, p model.Protocol) {
	if p != model.FPTN {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", containerName(id)).Run()
}

// InstallOpenFlux installs a pinned, digest-verified executable from upstream.
// It deliberately refuses mutable assets without a GitHub SHA-256 digest.
func InstallOpenFlux(ctx context.Context) error {
	externalVPNUpdateMu.Lock()
	defer externalVPNUpdateMu.Unlock()
	spec := releaseSpec{protocol: model.OpenFlux, api: "https://api.github.com/repos/p1neappleXpress/OpenFlux/releases/tags/v0.3.0", binaryName: "openflux"}
	rel, err := fetchLatestRelease(ctx, spec)
	if err != nil {
		return err
	}
	name := "openflux-linux-" + runtimeArch()
	var asset releaseAsset
	for _, a := range rel.Assets {
		if a.Name == name {
			asset = a
			break
		}
	}
	if asset.URL == "" || !strings.HasPrefix(asset.Digest, "sha256:") {
		return fmt.Errorf("OpenFlux release has no digest-verified %s asset", name)
	}
	dir := config.GetBinFolderPath()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".openflux-*")
	if err != nil {
		return err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	if err := downloadReleaseAsset(ctx, asset.URL, path); err != nil {
		return err
	}
	if err := verifyReleaseDigest(path, asset.Digest); err != nil {
		return err
	}
	if err := os.Chmod(path, 0755); err != nil {
		return err
	}
	// Upstream has no version flag: validate its help output rather than invoking
	// the default client, which would create network connections during install.
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(checkCtx, path, "--help").CombinedOutput()
	if err != nil || !strings.Contains(string(output), "--role") || !strings.Contains(string(output), "--config") {
		return fmt.Errorf("downloaded OpenFlux does not support managed configuration")
	}
	if err := os.Rename(path, filepath.Join(dir, "openflux")); err != nil {
		return err
	}
	GetManager().StopProtocol(model.OpenFlux)
	stopOutboundProtocol(model.OpenFlux)
	return nil
}

// Kept as a function to make the supported architecture explicit.
func runtimeArch() string { return runtime.GOARCH }
