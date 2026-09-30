package firewall

import (
	"context"
	"errors"
	"fmt"
	"runtime"
)

var errNoPackageManager = errors.New("no supported package manager found")

type packageManager struct {
	name   string
	binary string
	args   []string
}

var packageManagers = []packageManager{
	{name: "apt-get", binary: "apt-get", args: []string{"install", "-y", "ufw"}},
	{name: "dnf", binary: "dnf", args: []string{"install", "-y", "ufw"}},
	{name: "yum", binary: "yum", args: []string{"install", "-y", "ufw"}},
	{name: "pacman", binary: "pacman", args: []string{"-S", "--needed", "--noconfirm", "ufw"}},
	{name: "zypper", binary: "zypper", args: []string{"--non-interactive", "install", "ufw"}},
	{name: "apk", binary: "apk", args: []string{"add", "ufw"}},
}

func InstallUFW(ctx context.Context) (Status, error) {
	if runtime.GOOS != "linux" {
		return Status{Backend: BackendNone}, errors.New("ufw installation is only supported on Linux")
	}
	return installUFW(ctx, osCommandRunner{})
}

func installUFW(ctx context.Context, runner commandRunner) (Status, error) {
	if _, err := runner.LookPath("ufw"); err == nil {
		return detect(ctx, runner), nil
	}

	manager, path, err := findPackageManager(runner)
	if err != nil {
		return Status{Backend: BackendNone}, err
	}
	if err := runner.Run(ctx, path, manager.args...); err != nil {
		return Status{Backend: BackendNone}, fmt.Errorf("install ufw with %s: %w", manager.name, err)
	}
	if _, err := runner.LookPath("ufw"); err != nil {
		return Status{Backend: BackendNone}, errors.New("ufw installation completed but ufw executable was not found")
	}

	return detect(ctx, runner), nil
}

func findPackageManager(runner commandRunner) (packageManager, string, error) {
	for _, manager := range packageManagers {
		path, err := runner.LookPath(manager.binary)
		if err == nil {
			return manager, path, nil
		}
	}
	return packageManager{}, "", errNoPackageManager
}
