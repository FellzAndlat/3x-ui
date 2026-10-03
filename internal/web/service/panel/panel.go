package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/util/version"
	"github.com/SawaMEN/3x-ui/v3/internal/web/global"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

// PanelService provides business logic for panel management operations.
// It handles panel restart, updates, and system-level panel controls.
type PanelService struct{}

// PanelUpdateInfo contains the current and latest available panel versions.
// On the dev channel the version fields carry a "dev+<sha>" label and the commit
// fields hold the short SHAs that drive the update-available decision.
type PanelUpdateInfo struct {
	Channel         string `json:"channel"`
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	CurrentCommit   string `json:"currentCommit,omitempty"`
	LatestCommit    string `json:"latestCommit,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

const (
	panelUpdaterURL      = "https://raw.githubusercontent.com/SawaMEN/3x-ui/main/update.sh"
	panelUpdaterRunDir   = "/run/x-ui"
	maxPanelUpdaterBytes = 2 << 20
	// devReleaseTag is the fixed-tag rolling pre-release the CI force-moves to the
	// newest main commit; the dev update channel installs from it.
	devReleaseTag = "dev-latest"

	updateStatePending = "pending"
	updateStateSuccess = "success"
	updateStateFailed  = "failed"
)

// PanelUpdateStatus reports the outcome of the most recently launched panel
// self-update. RunID lets the caller confirm this status belongs to the
// update it started rather than a stale one.
type PanelUpdateStatus struct {
	RunID      string `json:"runId" example:"1735689600123456789"`
	State      string `json:"state" example:"success"`
	ExitCode   int    `json:"exitCode" example:"0"`
	Message    string `json:"message,omitempty" example:"failed to download x-ui release archive"`
	LogFile    string `json:"logFile,omitempty" example:"/var/log/x-ui/update.log"`
	FinishedAt int64  `json:"finishedAt" example:"1735689612"`
}

var releaseCommitRegex = regexp.MustCompile(`(?i)commit=([0-9a-f]{7,40})`)

var (
	updateMu      sync.Mutex
	updateRunning bool
	updateStarted time.Time
	updateRunID   int64
	updatePID     int
)

const (
	updateStaleAfter  = 20 * time.Minute
	updateHardCeiling = 2 * time.Hour
)

func (s *PanelService) RestartPanel(delay time.Duration) error {
	go func() {
		time.Sleep(delay)
		if global.TriggerRestart() {
			return
		}
		if runtime.GOOS == "windows" {
			logger.Error("panel restart: no restart hook registered (SIGHUP unsupported on Windows)")
			return
		}
		p, err := os.FindProcess(syscall.Getpid())
		if err != nil {
			logger.Error("panel restart: FindProcess failed:", err)
			return
		}
		if err := p.Signal(syscall.SIGHUP); err != nil {
			logger.Error("failed to send SIGHUP signal:", err)
		}
	}()
	return nil
}

func (s *PanelService) GetUpdateInfo() (*PanelUpdateInfo, error) {
	if devChannelActive() {
		return getDevUpdateInfo()
	}
	latest, err := fetchLatestPanelVersion()
	if err != nil {
		return nil, err
	}
	current := config.GetBaseVersion()
	return &PanelUpdateInfo{
		Channel:         "stable",
		CurrentVersion:  current,
		LatestVersion:   latest,
		UpdateAvailable: isNewerVersion(latest, current),
	}, nil
}

func devChannelActive() bool {
	enabled, err := (&service.SettingService{}).GetDevChannelEnable()
	return err == nil && enabled
}

func getDevUpdateInfo() (*PanelUpdateInfo, error) {
	release, err := fetchPanelRelease(devReleaseTag)
	if err != nil {
		return nil, err
	}
	latestCommit := extractReleaseCommit(release)
	if latestCommit == "" {
		return nil, fmt.Errorf("dev release commit is unknown")
	}
	currentCommit := config.GetBuildCommit()
	return &PanelUpdateInfo{
		Channel:         "dev",
		CurrentVersion:  config.GetPanelVersion(),
		CurrentCommit:   shortCommit(currentCommit),
		LatestCommit:    shortCommit(latestCommit),
		LatestVersion:   "dev+" + shortCommit(latestCommit),
		UpdateAvailable: !commitsEqual(currentCommit, latestCommit),
	}, nil
}

// AutoUpdateDevChannel checks the rolling dev release and starts a detached
// update only when the dev channel is enabled and a newer commit is available.
func (s *PanelService) AutoUpdateDevChannel() error {
	// Automatic rolling updates are only for an already installed dev build.
	// A stable build may opt into dev manually, but it must not be silently
	// treated as stale just because the dev channel is enabled.
	if !devChannelActive() || !config.IsDevBuild() {
		return nil
	}
	info, err := getDevUpdateInfo()
	if err != nil {
		return err
	}
	if !info.UpdateAvailable {
		return nil
	}

	runID, err := s.StartUpdateChannel(true)
	if err != nil {
		// Another update already running is a normal race with a manual update;
		// don't turn it into a noisy periodic error.
		if strings.Contains(err.Error(), "already in progress") {
			return nil
		}
		return err
	}
	logger.Infof("automatic dev panel update started: current=%s latest=%s run=%d", info.CurrentCommit, info.LatestCommit, runID)
	return nil
}

func (s *PanelService) StartUpdate() (int64, error) {
	return s.startUpdate(devChannelActive())
}

func (s *PanelService) StartUpdateChannel(dev bool) (int64, error) {
	return s.startUpdate(dev)
}

func (s *PanelService) GetUpdateStatus() *PanelUpdateStatus {
	data, err := os.ReadFile(config.GetUpdateStatusFilePath())
	if err != nil {
		return &PanelUpdateStatus{State: updateStatePending}
	}
	var status PanelUpdateStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return &PanelUpdateStatus{State: updateStatePending}
	}
	if status.State != updateStateSuccess && status.State != updateStateFailed {
		status.State = updateStatePending
	}
	return &status
}

func (s *PanelService) startUpdate(useDev bool) (int64, error) {
	runID := time.Now().UnixNano()
	if !acquireUpdateSlot(runID) {
		return 0, fmt.Errorf("a panel update is already in progress")
	}
	launched := false
	defer func() {
		if !launched {
			releaseUpdateSlot()
		}
	}()

	if runtime.GOOS != "linux" {
		return 0, fmt.Errorf("panel web update is supported only on Linux installations")
	}

	bash, err := exec.LookPath("bash")
	if err != nil {
		return 0, fmt.Errorf("bash is required to run the panel updater: %w", err)
	}

	scriptPath, err := downloadPanelUpdater()
	if err != nil {
		return 0, err
	}

	statusFile := config.GetUpdateStatusFilePath()
	mainFolder, serviceFolder := resolveUpdateFolders()
	updateTag := ""
	if useDev {
		updateTag = devReleaseTag
	}
	const updateLogFile = "/var/log/x-ui/update.log"
	_ = os.MkdirAll(filepath.Dir(updateLogFile), 0o755)
	updateScript := fmt.Sprintf(
		"set -e; exec >>%s 2>&1; trap 'rm -f %s' EXIT; echo '[x-ui] panel update started at '$(date -Is); %s %s",
		shellQuote(updateLogFile),
		shellQuote(scriptPath),
		shellQuote(bash),
		shellQuote(scriptPath),
	)
	runIDEnv := "XUI_UPDATE_RUN_ID=" + strconv.FormatInt(runID, 10)
	statusFileEnv := "XUI_UPDATE_STATUS_FILE=" + statusFile
	proxyEnv := updateProxyEnvVars()

	if systemdRun, err := exec.LookPath("systemd-run"); err == nil {
		unitName := fmt.Sprintf("x-ui-web-update-%d", time.Now().Unix())
		args := []string{
			"--unit", unitName,
			"--setenv", "XUI_MAIN_FOLDER=" + mainFolder,
			"--setenv", "XUI_SERVICE=" + serviceFolder,
			"--setenv", "XUI_UPDATE_TAG=" + updateTag,
			"--setenv", runIDEnv,
			"--setenv", statusFileEnv,
			"--setenv", "XUI_NONINTERACTIVE=1",
		}
		for _, kv := range proxyEnv {
			args = append(args, "--setenv", kv)
		}
		args = append(args, bash, "-lc", updateScript)
		cmd := exec.CommandContext(context.Background(), systemdRun, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			output := strings.TrimSpace(string(out))
			if !strings.Contains(output, "System has not been booted with systemd") && !strings.Contains(output, "Failed to connect to bus") {
				_ = os.Remove(scriptPath)
				return 0, fmt.Errorf("failed to start panel update job: %w: %s", err, output)
			}
			logger.Warning("systemd-run is unavailable, falling back to detached update process:", output)
		} else {
			logger.Infof("started panel update job via systemd-run unit %s", unitName)
			launched = true
			return runID, nil
		}
	}

	cmd := exec.CommandContext(context.Background(), bash, "-lc", updateScript)
	cmd.Env = append(os.Environ(),
		"XUI_MAIN_FOLDER="+mainFolder,
		"XUI_SERVICE="+serviceFolder,
		"XUI_UPDATE_TAG="+updateTag,
		runIDEnv,
		statusFileEnv,
		"XUI_NONINTERACTIVE=1",
	)
	setDetachedProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return 0, fmt.Errorf("failed to start panel update job: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		logger.Warning("failed to release panel update process:", err)
	}
	logger.Infof("started panel update job with pid %d", cmd.Process.Pid)
	recordUpdatePID(cmd.Process.Pid)
	launched = true
	return runID, nil
}

func updateProxyEnvVars() []string {
	var out []string
	for _, key := range []string{"https_proxy", "HTTPS_PROXY", "all_proxy", "ALL_PROXY", "http_proxy", "HTTP_PROXY", "no_proxy", "NO_PROXY"} {
		if v := os.Getenv(key); v != "" {
			out = append(out, key+"="+v)
		}
	}
	return out
}

func acquireUpdateSlot(runID int64) bool {
	updateMu.Lock()
	defer updateMu.Unlock()
	if updateRunning && !previousRunIsTerminal() {
		elapsed := time.Since(updateStarted)
		if elapsed < updateHardCeiling {
			stale := elapsed >= updateStaleAfter
			alive := updatePID > 0 && processAlive(updatePID)
			if !stale || alive {
				return false
			}
		}
	}
	updateRunning = true
	updateStarted = time.Now()
	updateRunID = runID
	updatePID = 0
	return true
}

func recordUpdatePID(pid int) {
	updateMu.Lock()
	updatePID = pid
	updateMu.Unlock()
}

func previousRunIsTerminal() bool {
	status := (&PanelService{}).GetUpdateStatus()
	return status.RunID == strconv.FormatInt(updateRunID, 10) && status.State != updateStatePending
}

func releaseUpdateSlot() {
	updateMu.Lock()
	updateRunning = false
	updateMu.Unlock()
}

func downloadPanelUpdater() (string, error) {
	client := (&service.SettingService{}).NewProxiedHTTPClient(15 * time.Second)
	req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, panelUpdaterURL, nil)
	if reqErr != nil {
		return "", fmt.Errorf("download panel updater: %w", reqErr)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download panel updater: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download panel updater: unexpected HTTP %d", resp.StatusCode)
	}

	// Arch's x-ui.service uses PrivateTmp=true. A script created with
	// os.CreateTemp("", ...) lands in the panel's private /tmp namespace, which
	// a detached systemd-run updater unit cannot see. Keep the updater in /run
	// instead: it is shared between units, root-owned, and naturally ephemeral.
	if err := os.MkdirAll(panelUpdaterRunDir, 0o700); err != nil {
		return "", fmt.Errorf("prepare panel updater runtime directory: %w", err)
	}
	file, err := os.CreateTemp(panelUpdaterRunDir, "3x-ui-update-*.sh")
	if err != nil {
		return "", fmt.Errorf("create panel updater temp file: %w", err)
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()

	n, err := io.Copy(file, io.LimitReader(resp.Body, maxPanelUpdaterBytes+1))
	if err != nil {
		return "", fmt.Errorf("write panel updater: %w", err)
	}
	if n == 0 {
		return "", fmt.Errorf("panel updater download is empty")
	}
	if n > maxPanelUpdaterBytes {
		return "", fmt.Errorf("panel updater exceeds %d bytes", maxPanelUpdaterBytes)
	}
	if err := file.Chmod(0o700); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

func fetchLatestPanelVersion() (string, error) {
	release, err := fetchPanelRelease("")
	if err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", fmt.Errorf("latest panel release tag is empty")
	}
	return release.TagName, nil
}

func fetchPanelRelease(tag string) (*service.Release, error) {
	url := "https://api.github.com/repos/SawaMEN/3x-ui/releases/latest"
	if tag != "" {
		url = "https://api.github.com/repos/SawaMEN/3x-ui/releases/tags/" + tag
	}
	client := (&service.SettingService{}).NewProxiedHTTPClient(10 * time.Second)
	req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if reqErr != nil {
		return nil, reqErr
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	var release service.Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func extractReleaseCommit(release *service.Release) string {
	if m := releaseCommitRegex.FindStringSubmatch(release.Body); m != nil {
		return strings.ToLower(m[1])
	}
	if isCommitSHA(release.TargetCommitish) {
		return strings.ToLower(release.TargetCommitish)
	}
	return ""
}

func isCommitSHA(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

func shortCommit(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func commitsEqual(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return strings.HasPrefix(b, a)
}

func resolveUpdateFolders() (string, string) {
	mainFolder := os.Getenv("XUI_MAIN_FOLDER")
	if mainFolder == "" {
		if exePath, err := os.Executable(); err == nil {
			mainFolder = filepath.Dir(exePath)
		}
	}
	if mainFolder == "" {
		mainFolder = "/usr/local/x-ui"
	}

	serviceFolder := os.Getenv("XUI_SERVICE")
	if serviceFolder == "" {
		serviceFolder = "/etc/systemd/system"
	}
	return mainFolder, serviceFolder
}

func isNewerVersion(latest string, current string) bool {
	cmp, ok := version.Compare(latest, current)
	if !ok {
		return version.Normalize(latest) != version.Normalize(current)
	}
	return cmp > 0
}

func compareVersionStrings(a, b string) (int, bool) {
	aParts, okA := parseVersionParts(a)
	bParts, okB := parseVersionParts(b)
	if !okA || !okB {
		return 0, false
	}
	for i := range len(aParts) {
		if aParts[i] > bParts[i] {
			return 1, true
		}
		if aParts[i] < bParts[i] {
			return -1, true
		}
	}
	return 0, true
}

func parseVersionParts(version string) ([3]int, bool) {
	var result [3]int
	parts := strings.Split(normalizeVersionTag(version), ".")
	if len(parts) != 3 {
		return result, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return result, false
		}
		result[i] = n
	}
	return result, true
}

func normalizeVersionTag(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
