package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	telemtMekoEnvPath         = "/etc/x-ui/telemt-meko-fix.env"
	telemtMekoScriptPath      = "/usr/local/x-ui/telemt-meko-fix.sh"
	telemtMekoUnitPath        = "/etc/systemd/system/telemt-meko-fix.service"
	telemtMekoBundledUnitPath = "/usr/local/x-ui/telemt-meko-fix.service"
)

type TelemtMekoConfig struct {
	Enabled       bool   `json:"enabled"`
	Applied       bool   `json:"applied"`
	RatePerMinute int    `json:"ratePerMinute"`
	Burst         int    `json:"burst"`
	Ports         []int  `json:"ports"`
	AppliedPorts  []int  `json:"appliedPorts"`
	Fingerprint   string `json:"fingerprint"`
	Mark          string `json:"mark"`
}

func defaultTelemtMekoConfig() TelemtMekoConfig {
	return TelemtMekoConfig{
		RatePerMinute: 54,
		Burst:         1,
		Fingerprint:   "MEKO V3/u32",
		Mark:          "0x400",
	}
}

func parseMekoBool(value string) bool {
	value = strings.ToLower(strings.TrimSpace(strings.Trim(value, "\"'")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func parseMekoRate(value string) int {
	value = strings.TrimSpace(strings.Trim(value, "\"'"))
	value = strings.TrimSuffix(value, "/minute")
	value = strings.TrimSuffix(value, "/min")
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

func parseMekoPortList(value string) []int {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	seen := make(map[int]struct{})
	for _, part := range strings.Split(value, ",") {
		p, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && p >= 1 && p <= 65535 {
			seen[p] = struct{}{}
		}
	}
	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

func readTelemtMekoEnv(cfg *TelemtMekoConfig) {
	data, err := os.ReadFile(telemtMekoEnvPath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "TELEMT_MEKO_ENABLED":
			cfg.Enabled = parseMekoBool(value)
		case "TELEMT_MEKO_RATE":
			if n := parseMekoRate(value); n > 0 {
				cfg.RatePerMinute = n
			}
		case "TELEMT_MEKO_BURST":
			if n, err := strconv.Atoi(strings.TrimSpace(strings.Trim(value, "\"'"))); err == nil && n > 0 {
				cfg.Burst = n
			}
		}
	}
}

func telemtPortFromConfig(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var raw struct {
		Server struct {
			Port int `toml:"port"`
		} `toml:"server"`
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return 0
	}
	if raw.Server.Port < 1 || raw.Server.Port > 65535 {
		return 0
	}
	return raw.Server.Port
}

func discoverTelemtMekoPorts() []int {
	seen := make(map[int]struct{})
	pattern := filepath.Join(filepath.Dir(telemtBinaryPath), "mtproto", "telemt-*.toml")
	if files, err := filepath.Glob(pattern); err == nil {
		for _, path := range files {
			if port := telemtPortFromConfig(path); port > 0 {
				seen[port] = struct{}{}
			}
		}
	}
	// The standalone Telemt page can still run its own service for WEB Proxy.
	// Cover that port as well, but only while the standalone service is active.
	if systemctl("is-active", "--quiet", telemtServiceName) == nil {
		if port := telemtPortFromConfig(telemtConfigPath); port > 0 {
			seen[port] = struct{}{}
		}
	}
	ports := make([]int, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

func mekoEnv(cfg TelemtMekoConfig) []string {
	enabled := "0"
	if cfg.Enabled {
		enabled = "1"
	}
	return []string{
		"TELEMT_MEKO_ENABLED=" + enabled,
		fmt.Sprintf("TELEMT_MEKO_RATE=%d/minute", cfg.RatePerMinute),
		fmt.Sprintf("TELEMT_MEKO_BURST=%d", cfg.Burst),
	}
}

func runTelemtMekoScript(action string, cfg TelemtMekoConfig) ([]byte, error) {
	if _, err := os.Stat(telemtMekoScriptPath); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(context.Background(), telemtMekoScriptPath, action)
	cmd.Env = append(os.Environ(), mekoEnv(cfg)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("telemt meko: %s failed: %w: %s", action, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func readTelemtMekoRuntime(cfg *TelemtMekoConfig) {
	out, err := runTelemtMekoScript("status", *cfg)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "installed":
			cfg.Applied = value == "true"
		case "ports":
			cfg.Ports = parseMekoPortList(value)
		case "applied_ports":
			cfg.AppliedPorts = parseMekoPortList(value)
		}
	}
}

func (TelemtService) GetMekoConfig() TelemtMekoConfig {
	cfg := defaultTelemtMekoConfig()
	readTelemtMekoEnv(&cfg)
	cfg.Ports = discoverTelemtMekoPorts()
	readTelemtMekoRuntime(&cfg)
	if cfg.Ports == nil {
		cfg.Ports = []int{}
	}
	if cfg.AppliedPorts == nil {
		cfg.AppliedPorts = []int{}
	}
	return cfg
}

func validateTelemtMekoConfig(cfg TelemtMekoConfig) error {
	if cfg.RatePerMinute < 1 || cfg.RatePerMinute > 60000 {
		return errors.New("telemt meko: rate must be between 1 and 60000 SYN/minute per IP")
	}
	if cfg.Burst < 1 || cfg.Burst > 1000 {
		return errors.New("telemt meko: burst must be between 1 and 1000")
	}
	return nil
}

func writeTelemtMekoEnv(cfg TelemtMekoConfig) error {
	if err := os.MkdirAll(filepath.Dir(telemtMekoEnvPath), 0o700); err != nil {
		return err
	}
	enabled := 0
	if cfg.Enabled {
		enabled = 1
	}
	data := fmt.Sprintf("TELEMT_MEKO_ENABLED=%d\nTELEMT_MEKO_RATE=%d/minute\nTELEMT_MEKO_BURST=%d\n", enabled, cfg.RatePerMinute, cfg.Burst)
	tmp, err := os.CreateTemp(filepath.Dir(telemtMekoEnvPath), ".telemt-meko-*.env")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, telemtMekoEnvPath)
}

func ensureTelemtMekoAssets() error {
	if info, err := os.Stat(telemtMekoScriptPath); err != nil {
		return fmt.Errorf("telemt meko: fix script is missing; update/reinstall the panel package: %w", err)
	} else if !info.Mode().IsRegular() {
		return errors.New("telemt meko: fix script is not a regular file")
	} else if info.Mode().Perm()&0o111 == 0 {
		if err := os.Chmod(telemtMekoScriptPath, 0o755); err != nil {
			return err
		}
	}

	bundled, err := os.ReadFile(telemtMekoBundledUnitPath)
	if err != nil {
		return fmt.Errorf("telemt meko: service unit is missing: %w", err)
	}
	installed, readErr := os.ReadFile(telemtMekoUnitPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("telemt meko: read installed service unit: %w", readErr)
	}
	if errors.Is(readErr, os.ErrNotExist) || string(installed) != string(bundled) {
		if err := os.WriteFile(telemtMekoUnitPath, bundled, 0o644); err != nil {
			return fmt.Errorf("telemt meko: install service unit: %w", err)
		}
	}
	return systemctl("daemon-reload")
}

func (TelemtService) SaveMekoConfig(cfg TelemtMekoConfig) error {
	if err := validateTelemtMekoConfig(cfg); err != nil {
		return err
	}
	if err := writeTelemtMekoEnv(cfg); err != nil {
		return fmt.Errorf("telemt meko: save settings: %w", err)
	}
	if cfg.Enabled {
		if err := ensureTelemtMekoAssets(); err != nil {
			return err
		}
		if err := systemctl("enable", telemtMekoServiceName); err != nil {
			return fmt.Errorf("telemt meko: enable failed: %w", err)
		}
		if err := systemctl("restart", telemtMekoServiceName); err != nil {
			return fmt.Errorf("telemt meko: apply failed: %w", err)
		}
		return nil
	}

	var errs []error
	if _, err := os.Stat(telemtMekoUnitPath); err == nil {
		if err := systemctl("disable", "--now", telemtMekoServiceName); err != nil {
			errs = append(errs, fmt.Errorf("telemt meko: disable failed: %w", err))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("telemt meko: inspect service unit: %w", err))
	}
	if _, err := os.Stat(telemtMekoScriptPath); err == nil {
		if _, err := runTelemtMekoScript("remove", cfg); err != nil {
			errs = append(errs, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("telemt meko: inspect fix script: %w", err))
	}
	return errors.Join(errs...)
}

func sameMekoPorts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// RefreshMekoFix reconciles the MEKO firewall rules with the Telemt processes
// currently generated from MTProto inbounds. It is intentionally a no-op when
// the configured and applied port sets already match, so the hashlimit buckets
// are not reset by every MTProto traffic polling cycle.
func (TelemtService) RefreshMekoFix() error {
	cfg := TelemtService{}.GetMekoConfig()
	if !cfg.Enabled {
		if cfg.Applied {
			_, err := runTelemtMekoScript("remove", cfg)
			return err
		}
		return nil
	}
	if cfg.Applied && sameMekoPorts(cfg.Ports, cfg.AppliedPorts) {
		return nil
	}
	if err := ensureTelemtMekoAssets(); err != nil {
		return err
	}
	_, err := runTelemtMekoScript("apply", cfg)
	return err
}
