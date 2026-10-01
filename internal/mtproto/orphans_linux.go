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

// killStrayMtgProcesses keeps its historical name for compatibility with the
// manager/tests, but now targets only panel-owned Telemt MTProto sidecars. The
// standalone Telemt service managed by the /telemt page uses the same binary
// with /etc/x-ui/telemt.toml and must never be killed here.
func killStrayMtgProcesses(binaryPath string) int {
	base := filepath.Base(binaryPath)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return 0
	}
	self := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	ownedDir, _ := filepath.Abs(configDir())
	killed := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		if procExeBase(pid) != base && cmdlineArgv0Base(pid) != base {
			continue
		}
		args := procCmdline(pid)
		if !isPanelTelemtSidecar(args, ownedDir) {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err == nil {
			killed++
		}
	}
	return killed
}

func isPanelTelemtSidecar(args []string, ownedDir string) bool {
	for _, arg := range args[1:] {
		if !strings.HasSuffix(arg, ".toml") {
			continue
		}
		abs, err := filepath.Abs(arg)
		if err != nil {
			continue
		}
		if filepath.Dir(abs) == ownedDir && strings.HasPrefix(filepath.Base(abs), "telemt-") {
			return true
		}
	}
	return false
}

func procExeBase(pid int) string {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ""
	}
	return filepath.Base(strings.TrimSuffix(exe, " (deleted)"))
}

func cmdlineArgv0Base(pid int) string {
	args := procCmdline(pid)
	if len(args) == 0 {
		return ""
	}
	return filepath.Base(args[0])
}

func procCmdline(pid int) []string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(data) == 0 {
		return nil
	}
	parts := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
