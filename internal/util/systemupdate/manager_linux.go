//go:build linux

package systemupdate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/config"
)

const commandTimeout = 30 * time.Minute

var updateMu sync.Mutex

type distroInfo struct {
	id      string
	version string
	manager string
}

func (d distroInfo) distributionForPackages() string {
	if d.id == "armbian" {
		return "armbian"
	}
	if d.manager == "apt-get" {
		return "ubuntu"
	}
	if d.manager == "dnf" || d.manager == "yum" {
		return "rhel"
	}
	if d.manager == "pacman" {
		return "arch"
	}
	if d.manager == "zypper" {
		return "opensuse-leap"
	}
	if d.manager == "apk" {
		return "alpine"
	}
	return d.id
}

var packageVersionPattern = regexp.MustCompile("^(.+)-([0-9][^[:space:]]*)[[:space:]]+<[[:space:]]+(.+)$")

func GetStatus(ctx context.Context) (Status, error) {
	info := detectDistribution()
	status := Status{
		Distribution:   info.id,
		Version:        info.version,
		PackageManager: info.manager,
		Supported:      info.manager != "",
		RunningAsRoot:  os.Geteuid() == 0,
	}
	if !status.Supported {
		return status, fmt.Errorf("unsupported Linux distribution or package manager")
	}

	upgrades, err := listAvailableUpdates(ctx, info.manager)
	if err != nil {
		return status, err
	}

	installedVersions, _ := installedPackageVersions(info.manager)
	packages, kernelPackages, missingPackages := collectPackageStatuses(
		info.distributionForPackages(),
		upgrades,
		func(name string) (string, bool) {
			if version, ok := installedVersions[name]; ok {
				return version, true
			}
			return installedPackageVersion(info.manager, name)
		},
	)
	status.Packages = packages
	status.MissingPackages = missingPackages

	kernel := KernelStatus{RunningVersion: runtimeKernelVersion()}
	kernel.RebootRequired = rebootRequired(info.manager)

	for _, item := range kernelPackages {
		if !item.UpdateAvailable {
			continue
		}
		kernel.UpdateAvailable = true
		kernel.PackageNames = append(kernel.PackageNames, item.Name)
		if kernel.AvailableVersion == "" {
			kernel.AvailableVersion = item.AvailableVersion
		} else if strings.Compare(kernel.AvailableVersion, item.AvailableVersion) < 0 {
			kernel.AvailableVersion = item.AvailableVersion
		}
	}
	sort.Strings(kernel.PackageNames)
	status.Kernel = kernel
	status.UpdatesAvailable = kernel.UpdateAvailable
	for _, item := range status.Packages {
		if item.UpdateAvailable {
			status.UpdatesAvailable = true
		}
	}
	status.CanUpdate = status.RunningAsRoot && (status.UpdatesAvailable || status.MissingPackages)

	if info.manager == "pacman" {
		status.Notes = append(status.Notes, "Arch Linux требует полного обновления системы через pacman -Syu; частичные обновления не поддерживаются.")
	}
	status.Notes = append(status.Notes,
		"Зависимости новых протоколов: WireGuard, AmneziaWG и VK-Turn используют iproute2/iproute и iptables для сетевого стека и маршрутизации; MTProto/Telemt, TUIC, Naive, Mieru и Psiphon используют curl, tar, xz, ca-certificates, openssl и socat для загрузки/запуска и TLS/туннельного окружения.",
		"Для Hysteria/TUIC/Naive и TLS-протоколов требуются актуальные ca-certificates и openssl; для UDP-маршрутизации и порт-хоппинга используются iproute2/iproute и iptables. Отдельные wireguard-tools и kernel-модули WireGuard здесь не требуются: соответствующие протоколы обслуживаются самим Xray/sidecar.",
	)
	if kernel.UpdateAvailable {
		status.Notes = append(status.Notes, "После обновления ядра потребуется перезагрузка сервера, чтобы запустить новое ядро.")
	}
	if kernel.RebootRequired {
		status.Notes = append(status.Notes, "Для применения уже установленного обновления системы требуется перезагрузка.")
	}
	if !status.RunningAsRoot {
		status.Notes = append(status.Notes, "Для установки обновлений панель должна работать с root-правами.")
	}
	return status, nil
}
func Refresh(ctx context.Context) (Status, error) {
	info := detectDistribution()
	if info.manager == "" {
		return Status{}, fmt.Errorf("unsupported Linux distribution or package manager")
	}

	refreshErr := refreshPackageDatabase(ctx, info.manager)
	status, statusErr := GetStatus(ctx)
	if statusErr != nil {
		if refreshErr != nil {
			return status, fmt.Errorf("package metadata refresh failed: %w; status check failed: %v", refreshErr, statusErr)
		}
		return status, statusErr
	}
	if refreshErr != nil {
		status.Notes = append(status.Notes,
			"Не удалось обновить локальный индекс пакетов; ниже показаны обновления из уже сохранённого кэша. Проверьте сетевое соединение и повторите проверку.",
		)
	}
	return status, nil
}

func newUpdateContext(_ context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), commandTimeout)
}

func Apply(ctx context.Context) (UpdateResult, error) {
	updateMu.Lock()
	defer updateMu.Unlock()

	if os.Geteuid() != 0 {
		return UpdateResult{}, fmt.Errorf("system updates require root privileges")
	}

	info := detectDistribution()
	if info.manager == "" {
		return UpdateResult{}, fmt.Errorf("unsupported Linux distribution or package manager")
	}

	updateCtx, cancel := newUpdateContext(ctx)
	defer cancel()

	if err := refreshPackageDatabase(updateCtx, info.manager); err != nil {
		return UpdateResult{}, err
	}
	status, err := GetStatus(updateCtx)
	if err != nil {
		return UpdateResult{}, err
	}

	missing := make([]string, 0)
	for _, item := range status.Packages {
		if item.Required && !item.Installed {
			missing = append(missing, item.Name)
		}
	}
	sort.Strings(missing)

	outputs := make([]string, 0, 2)
	if len(missing) > 0 && info.manager != "pacman" {
		args, ok := packageInstallCommand(info.manager, missing)
		if !ok {
			return UpdateResult{}, fmt.Errorf("unsupported package install command for %s", info.manager)
		}
		output, installErr := runCommand(updateCtx, args[0], args[1:]...)
		outputs = append(outputs, output)
		if installErr != nil {
			result := UpdateResult{
				Updated:        false,
				Output:         truncateOutput(strings.Join(outputs, "\n"), 20000),
				RebootRequired: status.Kernel.RebootRequired,
				Error:          truncateOutput(installErr.Error(), 4000),
			}
			return result, fmt.Errorf("required package installation failed: %s", result.Error)
		}
	}

	upgradeArgs := packageUpgradeCommand(info.manager)
	if info.manager == "pacman" && len(missing) > 0 {
		upgradeArgs = packageUpgradeCommand(info.manager, missing)
	}
	output, upgradeErr := runCommand(updateCtx, upgradeArgs[0], upgradeArgs[1:]...)
	outputs = append(outputs, output)

	result := UpdateResult{
		Updated:        upgradeErr == nil,
		Output:         truncateOutput(strings.Join(outputs, "\n"), 20000),
		RebootRequired: status.Kernel.UpdateAvailable || status.Kernel.RebootRequired,
	}
	if upgradeErr != nil {
		result.Error = truncateOutput(upgradeErr.Error(), 4000)
		return result, fmt.Errorf("system update failed: %s", result.Error)
	}

	result.RebootRequired = result.RebootRequired || rebootRequired(info.manager)
	return result, nil
}

func packageInstallCommand(manager string, names []string) ([]string, bool) {
	if len(names) == 0 {
		return nil, true
	}
	switch manager {
	case "apt-get":
		return append([]string{"apt-get", "install", "-y", "--no-install-recommends"}, names...), true
	case "dnf":
		return append([]string{"dnf", "install", "-y"}, names...), true
	case "yum":
		return append([]string{"yum", "install", "-y"}, names...), true
	case "zypper":
		return append([]string{"zypper", "--non-interactive", "install", "-y"}, names...), true
	case "apk":
		return append([]string{"apk", "add", "--no-cache"}, names...), true
	default:
		return nil, false
	}
}

func packageUpgradeCommand(manager string, names ...[]string) []string {
	selected := []string{}
	if len(names) > 0 {
		selected = names[0]
	}

	switch manager {
	case "apt-get":
		if len(selected) > 0 {
			return append([]string{"apt-get", "install", "-y", "--no-install-recommends"}, selected...)
		}
		return []string{"apt-get", "upgrade", "-y", "--with-new-pkgs", "--no-install-recommends"}
	case "dnf":
		if len(selected) > 0 {
			return append([]string{"dnf", "upgrade", "-y"}, selected...)
		}
		return []string{"dnf", "upgrade", "-y"}
	case "yum":
		if len(selected) > 0 {
			return append([]string{"yum", "update", "-y"}, selected...)
		}
		return []string{"yum", "update", "-y"}
	case "zypper":
		if len(selected) > 0 {
			return append([]string{"zypper", "--non-interactive", "update", "-y"}, selected...)
		}
		return []string{"zypper", "--non-interactive", "update", "-y"}
	case "apk":
		if len(selected) > 0 {
			return append([]string{"apk", "upgrade", "--no-cache"}, selected...)
		}
		return []string{"apk", "upgrade", "--no-cache"}
	case "pacman":
		if len(selected) > 0 {
			return append([]string{"pacman", "-Syu", "--noconfirm", "--needed"}, selected...)
		}
		return []string{"pacman", "-Syu", "--noconfirm", "--needed"}
	default:
		return []string{}
	}
}

func detectDistribution() distroInfo {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return distroInfo{}
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		values[parts[0]] = strings.Trim(strings.TrimSpace(parts[1]), "\"")
	}
	id := strings.ToLower(values["ID"])
	like := strings.ToLower(values["ID_LIKE"])
	switch {
	case id == "ubuntu" || id == "debian" || strings.Contains(like, "debian"):
		return distroInfo{id: id, version: values["VERSION_ID"], manager: "apt-get"}
	case id == "fedora":
		return distroInfo{id: id, version: values["VERSION_ID"], manager: "dnf"}
	case id == "amzn" || id == "rhel" || id == "almalinux" || id == "rocky" || id == "ol" || strings.Contains(like, "rhel") || strings.Contains(like, "fedora"):
		if commandExists("dnf") {
			return distroInfo{id: id, version: values["VERSION_ID"], manager: "dnf"}
		}
		return distroInfo{id: id, version: values["VERSION_ID"], manager: "yum"}
	case id == "centos":
		if commandExists("dnf") {
			return distroInfo{id: id, version: values["VERSION_ID"], manager: "dnf"}
		}
		return distroInfo{id: id, version: values["VERSION_ID"], manager: "yum"}
	case id == "arch" || strings.Contains(like, "arch") || id == "manjaro" || id == "parch":
		return distroInfo{id: id, version: values["VERSION_ID"], manager: "pacman"}
	case id == "opensuse-tumbleweed" || id == "opensuse-leap" || strings.Contains(id, "opensuse") || strings.Contains(like, "suse"):
		return distroInfo{id: id, version: values["VERSION_ID"], manager: "zypper"}
	case id == "alpine":
		return distroInfo{id: id, version: values["VERSION_ID"], manager: "apk"}
	default:
		if commandExists("apt-get") {
			return distroInfo{id: id, version: values["VERSION_ID"], manager: "apt-get"}
		}
		return distroInfo{}
	}
}

func requiredPackages(distribution string) []string {
	var packages []string
	switch distribution {
	case "ubuntu", "debian", "armbian":
		packages = []string{"cron", "curl", "tar", "xz-utils", "tzdata", "socat", "ca-certificates", "openssl", "util-linux", "iproute2", "iptables"}
	case "fedora", "amzn", "rhel", "almalinux", "rocky", "ol", "centos":
		packages = []string{"cronie", "curl", "tar", "xz", "tzdata", "socat", "ca-certificates", "openssl", "util-linux", "iproute", "iptables"}
	case "arch", "manjaro", "parch":
		packages = []string{"cronie", "curl", "tar", "xz", "tzdata", "socat", "ca-certificates", "openssl", "util-linux", "iproute2", "iptables"}
	case "opensuse-tumbleweed", "opensuse-leap":
		packages = []string{"cron", "curl", "tar", "xz", "timezone", "socat", "ca-certificates", "openssl", "util-linux", "iproute2", "iptables"}
	case "alpine":
		packages = []string{"dcron", "curl", "tar", "xz", "tzdata", "socat", "ca-certificates", "openssl", "util-linux", "iproute2", "iptables"}
	default:
		packages = []string{"cron", "curl", "tar", "xz-utils", "tzdata", "socat", "ca-certificates", "openssl", "util-linux", "iproute2", "iptables"}
	}

	addPackage := func(name string) {
		if name == "" {
			return
		}
		for _, existing := range packages {
			if existing == name {
				return
			}
		}
		packages = append(packages, name)
	}

	// The panel can use pg_dump/pg_restore when PostgreSQL is selected. Mirror
	// the package names used by install.sh/update.sh so the system update page
	// can repair a missing client as well.
	if config.GetDBKind() == "postgres" {
		switch distribution {
		case "ubuntu", "debian", "armbian", "alpine":
			addPackage("postgresql-client")
		case "fedora", "amzn", "rhel", "almalinux", "rocky", "ol", "centos",
			"arch", "manjaro", "parch", "opensuse-tumbleweed", "opensuse-leap":
			addPackage("postgresql")
		}
	}

	// Fail2ban is optional. When the module is installed, nftables is needed on
	// minimal images because recent fail2ban defaults use its nftables action.
	if commandExists("fail2ban-client") {
		addPackage("fail2ban")
		addPackage("nftables")
	}
	if commandExists("nft") {
		addPackage("nftables")
	}
	if commandExists("ufw") && (distribution == "ubuntu" || distribution == "debian" || distribution == "armbian") {
		addPackage("ufw")
	}
	if commandExists("nginx") {
		addPackage("nginx")
	}

	return packages
}
func collectPackageStatuses(
	distribution string,
	upgrades map[string]string,
	lookup func(string) (string, bool),
) ([]PackageStatus, []PackageStatus, bool) {
	packages := make([]PackageStatus, 0, len(upgrades)+len(requiredPackages(distribution)))
	seen := make(map[string]bool)
	missing := false

	appendPackage := func(name, availableVersion string, required, kernel bool) {
		if name == "" || seen[name] {
			return
		}
		installedVersion, installed := lookup(name)
		item := PackageStatus{
			Name:             name,
			InstalledVersion: installedVersion,
			AvailableVersion: availableVersion,
			Installed:        installed,
			Required:         required,
			Kernel:           kernel,
			UpdateAvailable:  packageUpdateAvailable(installed, installedVersion, availableVersion),
		}
		if required && !installed {
			missing = true
		}
		packages = append(packages, item)
		seen[name] = true
	}

	for _, name := range requiredPackages(distribution) {
		appendPackage(name, upgrades[name], true, isKernelPackage(name))
	}

	for name, availableVersion := range upgrades {
		if seen[name] || !isKernelPackage(name) {
			continue
		}
		_, installed := lookup(name)
		if !installed {
			continue
		}
		appendPackage(name, availableVersion, false, true)
	}

	sort.Slice(packages, func(i, j int) bool {
		if packages[i].UpdateAvailable != packages[j].UpdateAvailable {
			return packages[i].UpdateAvailable
		}
		if packages[i].Kernel != packages[j].Kernel {
			return packages[i].Kernel
		}
		return packages[i].Name < packages[j].Name
	})

	kernelPackages := make([]PackageStatus, 0)
	for _, item := range packages {
		if item.Kernel {
			kernelPackages = append(kernelPackages, item)
		}
	}
	return packages, kernelPackages, missing
}

func packageUpdateAvailable(installed bool, installedVersion, availableVersion string) bool {
	return installed && availableVersion != "" && installedVersion != "" && installedVersion != availableVersion
}

func refreshPackageDatabase(ctx context.Context, manager string) error {
	switch manager {
	case "apt-get":
		_, err := runCommand(ctx, "apt-get", "update")
		return err
	case "dnf":
		_, err := runCommand(ctx, "dnf", "makecache", "-y")
		return err
	case "yum":
		_, err := runCommand(ctx, "yum", "makecache", "-y")
		return err
	case "zypper":
		_, err := runCommand(ctx, "zypper", "refresh")
		return err
	case "pacman":
		return nil
	case "apk":
		_, err := runCommand(ctx, "apk", "update")
		return err
	default:
		return fmt.Errorf("unsupported package manager: %s", manager)
	}
}

func listAvailableUpdates(ctx context.Context, manager string) (map[string]string, error) {
	switch manager {
	case "apt-get":
		output, err := runCommand(ctx, "apt", "list", "--upgradable")
		return parseAptUpdates(output), err
	case "dnf", "yum":
		output, err := runCommand(ctx, manager, "-q", "check-update")
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 100 {
				if strings.TrimSpace(output) == "" {
					return nil, err
				}
			}
		}
		return parseDnfUpdates(output), nil
	case "zypper":
		output, err := runCommand(ctx, "zypper", "--non-interactive", "list-updates")
		if err != nil && strings.TrimSpace(output) == "" {
			return nil, err
		}
		return parseZypperUpdates(output), nil
	case "pacman":
		if commandExists("checkupdates") {
			output, err := runCommand(ctx, "checkupdates")
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
					return nil, err
				}
			}
			return parsePacmanUpdates(output), nil
		}
		output, err := runCommand(ctx, "pacman", "-Qu")
		if err != nil && strings.TrimSpace(output) == "" {
			return nil, err
		}
		return parsePacmanQueryUpdates(output), nil
	case "apk":
		output, err := runCommand(ctx, "apk", "version", "-l", "<")
		if err != nil {
			return nil, err
		}
		return parseApkUpdates(output), nil
	default:
		return nil, fmt.Errorf("unsupported package manager: %s", manager)
	}
}

var installedApkPackagePattern = regexp.MustCompile("^(.+)-([0-9][^[:space:]]*)$")

func installedPackageVersions(manager string) (map[string]string, error) {
	result := map[string]string{}
	var output []byte
	var err error

	switch manager {
	case "apt-get":
		output, err = exec.CommandContext(context.Background(), "dpkg-query", "-W", "-f=${Package}\t${Version}\t${Status}\n").Output()
	case "dnf", "yum", "zypper":
		output, err = exec.CommandContext(context.Background(), "rpm", "-qa", "--qf", "%{NAME}\t%{VERSION}-%{RELEASE}\n").Output()
	case "pacman":
		output, err = exec.CommandContext(context.Background(), "pacman", "-Q").Output()
	case "apk":
		output, err = exec.CommandContext(context.Background(), "apk", "info", "-v").Output()
	default:
		return result, fmt.Errorf("unsupported package manager: %s", manager)
	}
	if err != nil {
		return result, err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		switch manager {
		case "apt-get":
			fields := strings.Split(line, "\t")
			if len(fields) >= 3 && fields[2] == "install ok installed" {
				result[fields[0]] = fields[1]
			}
		case "dnf", "yum", "zypper", "pacman":
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				result[fields[0]] = fields[1]
			}
		case "apk":
			match := installedApkPackagePattern.FindStringSubmatch(line)
			if len(match) == 3 {
				result[match[1]] = match[2]
			}
		}
	}
	return result, scanner.Err()
}

func installedPackageVersion(manager, name string) (string, bool) {
	switch manager {
	case "apt-get":
		output, err := exec.CommandContext(context.Background(), "dpkg-query", "-W", "-f=${Status}\t${Version}\n", name).Output()
		if err != nil {
			return "", false
		}
		fields := strings.Split(strings.TrimSpace(string(output)), "\t")
		if len(fields) < 2 || fields[0] != "install ok installed" {
			return "", false
		}
		return fields[1], true
	case "dnf", "yum", "zypper":
		output, err := exec.CommandContext(context.Background(), "rpm", "-q", "--qf", "%{VERSION}-%{RELEASE}", name).Output()
		if err != nil {
			return "", false
		}
		version := strings.TrimSpace(string(output))
		return version, version != ""
	case "pacman":
		output, err := exec.CommandContext(context.Background(), "pacman", "-Q", name).Output()
		if err != nil {
			return "", false
		}
		fields := strings.Fields(string(output))
		if len(fields) < 2 {
			return "", false
		}
		return fields[1], true
	case "apk":
		output, err := exec.CommandContext(context.Background(), "apk", "info", "-e", name).Output()
		if err != nil {
			return "", false
		}
		line := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
		if line == "" {
			return "", false
		}
		return strings.TrimPrefix(line, name+"-"), true
	default:
		return "", false
	}
}

func parseAptUpdates(output string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, "[upgradable from:") || !strings.Contains(line, "/") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.SplitN(fields[0], "/", 2)[0]
		if name != "Listing..." {
			result[name] = fields[1]
		}
	}
	return result
}

func parseDnfUpdates(output string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], "Last") || strings.HasPrefix(fields[0], "Obsoleting") {
			continue
		}
		name := fields[0]
		if idx := strings.LastIndex(name, "."); idx > 0 && isRPMArch(name[idx+1:]) {
			name = name[:idx]
		}
		result[name] = fields[1]
	}
	return result
}

func parseZypperUpdates(output string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "|")
		if len(parts) < 5 {
			continue
		}
		name := strings.TrimSpace(parts[2])
		version := strings.TrimSpace(parts[4])
		if name != "" && version != "" && name != "Name" {
			result[name] = version
		}
	}
	return result
}

func parsePacmanUpdates(output string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		result[fields[0]] = fields[3]
	}
	return result
}

func parsePacmanQueryUpdates(output string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 && fields[2] == "->" {
			result[fields[0]] = fields[3]
			continue
		}
		if len(fields) >= 3 {
			result[fields[0]] = fields[2]
		}
	}
	return result
}

func parseApkUpdates(output string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		match := packageVersionPattern.FindStringSubmatch(strings.TrimSpace(scanner.Text()))
		if len(match) != 4 {
			continue
		}
		result[match[1]] = match[3]
	}
	return result
}

func isRPMArch(value string) bool {
	switch value {
	case "x86_64", "aarch64", "noarch", "ppc64le", "s390x", "arm64", "armv7hl", "i686":
		return true
	default:
		return false
	}
}

func isKernelPackage(name string) bool {
	lower := strings.ToLower(name)
	for _, exact := range []string{"linux", "linux-hardened", "linux-zen", "linux-rt"} {
		if lower == exact {
			return true
		}
	}
	for _, prefix := range []string{
		"linux-image", "linux-generic", "linux-virtual", "linux-azure",
		"linux-oem", "linux-lowlatency", "linux-kvm", "linux-lts", "linux-headers",
		"linux-modules", "kernel",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func runtimeKernelVersion() string {
	output, err := exec.CommandContext(context.Background(), "uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

var startAsyncCommand = func(_ context.Context, command string, args ...string) error {
	return exec.Command(command, args...).Start()
}

// Reboot schedules a system reboot without waiting for the shutdown to finish.
// This is intentionally asynchronous because the HTTP request will be terminated
// by the reboot itself. The spawned process must not inherit the HTTP request
// context: that context is canceled as soon as the response is returned.
func Reboot(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("system reboot requires root privileges")
	}

	commands := [][]string{
		{"systemctl", "reboot"},
		{"reboot"},
		{"shutdown", "-r", "now"},
	}
	for _, args := range commands {
		if !commandExists(args[0]) {
			continue
		}
		if err := startAsyncCommand(ctx, args[0], args[1:]...); err == nil {
			return nil
		}
	}
	return fmt.Errorf("failed to start system reboot command")
}

func rebootRequired(manager string) bool {
	for _, path := range []string{"/var/run/reboot-required", "/run/reboot-required"} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	if manager == "dnf" || manager == "yum" {
		if commandExists("needs-restarting") {
			return exec.CommandContext(context.Background(), "needs-restarting", "-r").Run() != nil
		}
	}
	return false
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runCommand(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "LANGUAGE=C")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func truncateOutput(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max] + "\n…"
}