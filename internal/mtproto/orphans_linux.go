//go:build linux

package mtproto

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// killStrayTelemtSidecars reaps only Telemt processes owned by the MTProto
// sidecar manager. Matching the binary name alone is unsafe because the same
// telemt binary can also be used by telemt.service / WEB Proxy.
func killStrayTelemtSidecars(binaryPath string) int {
	base := filepath.Base(binaryPath)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return 0
	}
	self := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	killed := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		if procExeBase(pid) != base && cmdlineArgv0Base(pid) != base {
			continue
		}
		if !isOwnedTelemtSidecar(pid) {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err == nil {
			killed++
		}
	}
	return killed
}

func isOwnedTelemtSidecar(pid int) bool {
	args := cmdlineArgs(pid)
	if len(args) < 3 || args[1] != "run" {
		return false
	}
	cfg := filepath.Clean(args[2])
	ownedDir := filepath.Clean(configDir())
	if filepath.Dir(cfg) != ownedDir {
		return false
	}
	name := filepath.Base(cfg)
	if !strings.HasPrefix(name, "telemt-") || !strings.HasSuffix(name, ".toml") {
		return false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, "telemt-"), ".toml")
	_, err := strconv.Atoi(id)
	return err == nil && id != ""
}

// procExeBase returns the base name of /proc/<pid>/exe, or "" if unreadable.
func procExeBase(pid int) string {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ""
	}
	return filepath.Base(strings.TrimSuffix(exe, " (deleted)"))
}

func cmdlineArgs(pid int) []string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(data) == 0 {
		return nil
	}
	raw := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	args := make([]string, 0, len(raw))
	for _, arg := range raw {
		if arg != "" {
			args = append(args, arg)
		}
	}
	return args
}

// cmdlineArgv0Base returns the base name of argv[0] from /proc/<pid>/cmdline,
// the reliable fallback when the binary has been replaced or exe is unreadable.
func cmdlineArgv0Base(pid int) string {
	args := cmdlineArgs(pid)
	if len(args) == 0 {
		return ""
	}
	return filepath.Base(args[0])
}
