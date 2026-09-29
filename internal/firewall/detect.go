package firewall

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
)

type Backend string

const (
	BackendNone      Backend = "none"
	BackendUFW       Backend = "ufw"
	BackendFirewalld Backend = "firewalld"
	BackendNftables  Backend = "nftables"
	BackendIptables  Backend = "iptables"
)

type Status struct {
	Backend   Backend `json:"backend"`
	Installed bool    `json:"installed"`
	Active    bool    `json:"active"`
}

type candidate struct {
	backend Backend
	binary  string
	args    []string
	active  func(string) bool
}

var candidates = []candidate{
	{
		backend: BackendUFW,
		binary:  "ufw",
		args:    []string{"status"},
		active: func(output string) bool {
			return strings.Contains(strings.ToLower(output), "status: active")
		},
	},
	{
		backend: BackendFirewalld,
		binary:  "firewall-cmd",
		args:    []string{"--state"},
		active: func(output string) bool {
			return strings.TrimSpace(output) == "running"
		},
	},
	{
		backend: BackendNftables,
		binary:  "nft",
		args:    []string{"list", "ruleset"},
		active: func(output string) bool {
			return strings.TrimSpace(output) != ""
		},
	},
	{
		backend: BackendIptables,
		binary:  "iptables",
		args:    []string{"-S"},
		active:  iptablesActive,
	},
}

func Detect(ctx context.Context) Status {
	if runtime.GOOS != "linux" {
		return Status{Backend: BackendNone}
	}

	installed := make([]candidate, 0, len(candidates))
	for _, item := range candidates {
		path, err := exec.LookPath(item.binary)
		if err != nil {
			continue
		}

		installed = append(installed, item)
		output, err := exec.CommandContext(ctx, path, item.args...).CombinedOutput()
		if err == nil && item.active(string(output)) {
			return Status{Backend: item.backend, Installed: true, Active: true}
		}
	}

	if len(installed) > 0 {
		return Status{Backend: installed[0].backend, Installed: true}
	}

	return Status{Backend: BackendNone}
}

func iptablesActive(output string) bool {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-A ") {
			return true
		}
		if strings.HasPrefix(line, "-P ") && !strings.HasSuffix(line, " ACCEPT") {
			return true
		}
	}
	return false
}
