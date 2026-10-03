//go:build linux

package systemupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRequiredPackagesIncludeNetworkProtocolDependencies(t *testing.T) {
	for _, distro := range []string{"ubuntu", "debian", "armbian", "fedora", "rhel", "arch", "opensuse-leap", "alpine"} {
		packages := requiredPackages(distro)
		seen := map[string]bool{}
		for _, name := range packages {
			seen[name] = true
		}
		for _, want := range []string{"iproute2", "iptables"} {
			if distro == "fedora" || distro == "rhel" {
				if want == "iproute2" {
					want = "iproute"
				}
			}
			if !seen[want] {
				t.Fatalf("%s requiredPackages() missing %s: %#v", distro, want, packages)
			}
		}
	}
}

func TestRequiredPackagesCoverProtocolRuntimeDependencies(t *testing.T) {
	cases := map[string][]string{
		"ubuntu":        {"iproute2", "iptables", "socat", "curl", "tar", "ca-certificates", "openssl"},
		"debian":        {"iproute2", "iptables", "socat", "curl", "tar", "ca-certificates", "openssl"},
		"armbian":       {"iproute2", "iptables", "socat", "curl", "tar", "ca-certificates", "openssl"},
		"fedora":        {"iproute", "iptables", "socat", "curl", "tar", "ca-certificates", "openssl"},
		"rhel":          {"iproute", "iptables", "socat", "curl", "tar", "ca-certificates", "openssl"},
		"centos":        {"iproute", "iptables", "socat", "curl", "tar", "ca-certificates", "openssl"},
		"arch":          {"iproute2", "iptables", "socat", "curl", "tar", "ca-certificates", "openssl"},
		"opensuse-leap": {"iproute2", "iptables", "socat", "curl", "tar", "ca-certificates", "openssl"},
		"alpine":        {"iproute2", "iptables", "socat", "curl", "tar", "ca-certificates", "openssl"},
	}
	for distro, wantPackages := range cases {
		packages := requiredPackages(distro)
		seen := map[string]bool{}
		for _, name := range packages {
			seen[name] = true
		}
		for _, want := range wantPackages {
			if !seen[want] {
				t.Fatalf("%s requiredPackages() missing %s: %#v", distro, want, packages)
			}
		}
	}
}

func TestPackageUpdateAvailable(t *testing.T) {
	tests := []struct {
		name      string
		installed bool
		current   string
		available string
		want      bool
	}{
		{name: "same version", installed: true, current: "6.8.0-31-generic", available: "6.8.0-31-generic", want: false},
		{name: "different version", installed: true, current: "6.8.0-30-generic", available: "6.8.0-31-generic", want: true},
		{name: "not installed", installed: false, current: "", available: "6.8.0-31-generic", want: false},
		{name: "missing available", installed: true, current: "6.8.0-31-generic", available: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := packageUpdateAvailable(tt.installed, tt.current, tt.available); got != tt.want {
				t.Fatalf("packageUpdateAvailable(%t, %q, %q) = %t, want %t", tt.installed, tt.current, tt.available, got, tt.want)
			}
		})
	}
}

func TestParseAptUpdates(t *testing.T) {
	got := parseAptUpdates(`Listing... Done
curl/noble-updates 8.5.0-1 amd64 [upgradable from: 8.4.0-1]
openssl/noble-updates 3.0.0 amd64 [upgradable from: 2.9.0]
`)
	if got["curl"] != "8.5.0-1" || got["openssl"] != "3.0.0" {
		t.Fatalf("parseAptUpdates() = %#v", got)
	}
}

func TestParseDnfUpdates(t *testing.T) {
	got := parseDnfUpdates(`kernel-core.x86_64 6.12.1-1.el9 baseos
curl.x86_64 8.5.0-1 appstream
Last metadata expiration check: 1:00:00 ago
`)
	if got["kernel-core"] != "6.12.1-1.el9" || got["curl"] != "8.5.0-1" {
		t.Fatalf("parseDnfUpdates() = %#v", got)
	}
}

func TestParseZypperUpdates(t *testing.T) {
	got := parseZypperUpdates(`v | repo | Name | Current Version | Available Version | Arch
--+------+------+
v | main | curl | 8.4.0 | 8.5.0 | x86_64
`)
	if got["curl"] != "8.5.0" {
		t.Fatalf("parseZypperUpdates() = %#v", got)
	}
}

func TestParsePacmanUpdates(t *testing.T) {
	got := parsePacmanUpdates(`curl 8.4.0-1 -> 8.5.0-1
linux 6.10.1-1 -> 6.10.2-1
`)
	if got["curl"] != "8.5.0-1" || got["linux"] != "6.10.2-1" {
		t.Fatalf("parsePacmanUpdates() = %#v", got)
	}
}

func TestParsePacmanQueryUpdates(t *testing.T) {
	got := parsePacmanQueryUpdates(`curl 8.4.0-1 -> 8.5.0-1
linux 6.10.1-1 -> 6.10.2-1
`)
	if got["curl"] != "8.5.0-1" || got["linux"] != "6.10.2-1" {
		t.Fatalf("parsePacmanQueryUpdates() = %#v", got)
	}
}

func TestParseApkUpdates(t *testing.T) {
	got := parseApkUpdates(`curl-8.4.0-r0 < 8.5.0-r0
linux-lts-6.6.1-r0 < 6.6.2-r0
`)
	if got["curl"] != "8.5.0-r0" || got["linux-lts"] != "6.6.2-r0" {
		t.Fatalf("parseApkUpdates() = %#v", got)
	}
}

func TestIsKernelPackage(t *testing.T) {
	for _, name := range []string{"linux-image-generic", "linux-lts", "kernel-core", "kernel-default"} {
		if !isKernelPackage(name) {
			t.Fatalf("isKernelPackage(%q) = false", name)
		}
	}
	for _, name := range []string{"curl", "openssl", "tzdata"} {
		if isKernelPackage(name) {
			t.Fatalf("isKernelPackage(%q) = true", name)
		}
	}
}

func TestStartAsyncCommandIgnoresCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	marker := filepath.Join(t.TempDir(), "started")
	err := startAsyncCommand(ctx, "sh", "-c", "printf started > \"$1\"", "sh", marker)
	if err != nil {
		t.Fatalf("startAsyncCommand() error = %v", err)
	}

	for i := 0; i < 50; i++ {
		if data, err := os.ReadFile(marker); err == nil && string(data) == "started" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("async command did not run after context cancellation")
}

func TestUpdateContextIgnoresCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	updateCtx, updateCancel := newUpdateContext(ctx)
	defer updateCancel()

	select {
	case <-updateCtx.Done():
		t.Fatalf("update context was canceled with the request context")
	default:
	}
}

func TestIsKernelPackageIncludesCommonArchKernels(t *testing.T) {
	for _, name := range []string{"linux", "linux-zen", "linux-hardened", "linux-rt"} {
		if !isKernelPackage(name) {
			t.Fatalf("isKernelPackage(%q) = false", name)
		}
	}
}

func TestCollectPackageStatusesIncludesOnlyRequiredAndKernelUpdates(t *testing.T) {
	lookup := func(name string) (string, bool) {
		return "1.0", true
	}
	upgrades := map[string]string{
		"curl":             "8.5.0",
		"linux-image-test": "6.2.0",
		"openssl":          "3.0.1",
		"bash":             "5.2.0",
	}

	packages, kernels, missing := collectPackageStatuses("ubuntu", upgrades, lookup, false)
	if missing {
		t.Fatalf("collectPackageStatuses() reported missing required packages")
	}

	byName := map[string]PackageStatus{}
	for _, item := range packages {
		byName[item.Name] = item
	}
	for _, name := range []string{"curl", "openssl"} {
		item, ok := byName[name]
		if !ok {
			t.Fatalf("collectPackageStatuses() omitted required package %q: %#v", name, packages)
		}
		if !item.UpdateAvailable || !item.Required {
			t.Fatalf("required package %q was not marked correctly: %#v", name, item)
		}
	}
	// An installed package without a pending upgrade still has an available
	// version, equal to the installed version.
	if item := byName["tar"]; item.AvailableVersion != "1.0" || item.UpdateAvailable {
		t.Fatalf("current package has incorrect available version: %#v", item)
	}
	for _, name := range []string{"bash"} {
		if _, ok := byName[name]; ok {
			t.Fatalf("collectPackageStatuses() exposed unrelated package %q: %#v", name, packages)
		}
	}
	kernel, ok := byName["linux-image-test"]
	if !ok || !kernel.UpdateAvailable || !kernel.Kernel {
		t.Fatalf("kernel package was not retained: %#v", packages)
	}
	if len(kernels) != 1 || kernels[0].Name != "linux-image-test" {
		t.Fatalf("kernel packages = %#v", kernels)
	}
}

func TestAllPackagesScopeShowsOnlyInstalledUpgrades(t *testing.T) {
	upgrades := map[string]string{"curl": "2", "bash": "2", "linux-image-test": "2", "not-installed": "2"}
	lookup := func(name string) (string, bool) {
		if name == "not-installed" {
			return "", false
		}
		return "1", true
	}
	packages, _, _ := collectPackageStatuses("ubuntu", upgrades, lookup, true)
	selected := packagesToUpgrade(packages, true)
	if got := strings.Join(selected, ","); got != "bash,curl,linux-image-test" {
		t.Fatalf("all packages upgrade = %s", got)
	}
	if got := strings.Join(packagesToUpgrade(packages, false), ","); got != "curl,linux-image-test" {
		t.Fatalf("panel packages upgrade = %s", got)
	}
}

func TestArchAlwaysUsesFullPackageScope(t *testing.T) {
	if !effectivePackageScope("pacman", false) || !effectivePackageScope("pacman", true) {
		t.Fatal("pacman must always use a full upgrade scope")
	}
	if effectivePackageScope("apt-get", false) {
		t.Fatal("other package managers must keep the requested scope")
	}
}

func TestPackageUpgradeCommand(t *testing.T) {
	names := []string{"curl", "openssl"}
	tests := map[string][]string{
		"apt-get": {"apt-get", "install", "-y", "--no-install-recommends", "curl", "openssl"},
		"dnf":     {"dnf", "upgrade", "-y", "curl", "openssl"},
		"yum":     {"yum", "update", "-y", "curl", "openssl"},
		"zypper":  {"zypper", "--non-interactive", "update", "-y", "curl", "openssl"},
		"apk":     {"apk", "upgrade", "--no-cache", "curl", "openssl"},
		"pacman":  {"pacman", "-Syu", "--noconfirm", "--needed", "curl", "openssl"},
	}
	for manager, want := range tests {
		got := packageUpgradeCommand(manager, names)
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("packageUpgradeCommand(%q) = %#v, want %#v", manager, got, want)
		}
	}
}

func TestDistroInfoPackageFamily(t *testing.T) {
	tests := []struct {
		info distroInfo
		want string
	}{
		{distroInfo{id: "ubuntu", manager: "apt-get"}, "ubuntu"},
		{distroInfo{id: "linuxmint", manager: "apt-get"}, "ubuntu"},
		{distroInfo{id: "rocky", manager: "dnf"}, "rhel"},
		{distroInfo{id: "manjaro", manager: "pacman"}, "arch"},
		{distroInfo{id: "opensuse-tumbleweed", manager: "zypper"}, "opensuse-leap"},
	}
	for _, tt := range tests {
		if got := tt.info.distributionForPackages(); got != tt.want {
			t.Fatalf("distributionForPackages() = %q, want %q for %#v", got, tt.want, tt.info)
		}
	}
}

func TestInstalledApkPackagePattern(t *testing.T) {
	cases := map[string]string{
		"curl-8.5.0-r0":           "curl",
		"linux-lts-6.6.90-r0":     "linux-lts",
		"ca-certificates-2025-r0": "ca-certificates",
	}
	for line, want := range cases {
		match := installedApkPackagePattern.FindStringSubmatch(line)
		if len(match) != 3 || match[1] != want || match[2] == "" {
			t.Fatalf("installedApkPackagePattern(%q) = %#v, want package %q", line, match, want)
		}
	}
}
