//go:build linux

package swap

import "testing"

func TestPackageInstallArgs(t *testing.T) {
	tests := []struct {
		name    string
		manager string
		pkg     string
		want    []string
	}{
		{
			name:    "apt",
			manager: "apt-get",
			pkg:     "systemd-zram-generator",
			want:    []string{"apt-get", "install", "-y", "systemd-zram-generator"},
		},
		{
			name:    "dnf",
			manager: "dnf",
			pkg:     "zram-generator",
			want:    []string{"dnf", "install", "-y", "zram-generator"},
		},
		{
			name:    "pacman",
			manager: "pacman",
			pkg:     "zram-generator",
			want:    []string{"pacman", "-S", "--noconfirm", "zram-generator"},
		},
		{
			name:    "apk",
			manager: "apk",
			pkg:     "zram-init",
			want:    []string{"apk", "add", "--no-cache", "zram-init"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := packageInstallArgs(tt.manager, tt.pkg)
			if len(got) != len(tt.want) {
				t.Fatalf("packageInstallArgs() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("packageInstallArgs() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestChooseDefaultAlgorithm(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{name: "prefers zstd", values: []string{"lzo-rle", "zstd", "lz4"}, want: "zstd"},
		{name: "falls back to lz4", values: []string{"lzo-rle", "lz4"}, want: "lz4"},
		{name: "uses first available", values: []string{"lzo-rle", "lzo"}, want: "lzo-rle"},
		{name: "uses zstd default when empty", values: nil, want: "zstd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chooseDefaultAlgorithm(tt.values); got != tt.want {
				t.Fatalf("chooseDefaultAlgorithm() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPackageReinstallArgs(t *testing.T) {
	tests := []struct {
		name    string
		manager string
		pkg     string
		want    []string
	}{
		{name: "apt", manager: "apt-get", pkg: "systemd-zram-generator", want: []string{"apt-get", "install", "-y", "--reinstall", "systemd-zram-generator"}},
		{name: "dnf", manager: "dnf", pkg: "zram-generator", want: []string{"dnf", "reinstall", "-y", "zram-generator"}},
		{name: "pacman", manager: "pacman", pkg: "zram-generator", want: []string{"pacman", "-S", "--noconfirm", "zram-generator"}},
		{name: "apk", manager: "apk", pkg: "zram-init", want: []string{"apk", "fix", "--no-cache", "zram-init"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := packageReinstallArgs(tt.manager, tt.pkg)
			if len(got) != len(tt.want) {
				t.Fatalf("packageReinstallArgs() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("packageReinstallArgs() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestZramPackageCandidates(t *testing.T) {
	tests := []struct {
		name         string
		distribution string
		recommended  string
		want         []string
	}{
		{
			name:         "ubuntu",
			distribution: "ubuntu",
			recommended:  "systemd-zram-generator",
			want:         []string{"systemd-zram-generator", "zram-config", "zram-tools"},
		},
		{
			name:         "fedora",
			distribution: "fedora",
			recommended:  "zram-generator-defaults",
			want:         []string{"zram-generator-defaults", "zram-generator"},
		},
		{
			name:         "armbian",
			distribution: "armbian",
			recommended:  "systemd-zram-generator",
			want:         []string{"systemd-zram-generator", "zram-config", "zram-tools"},
		},
		{
			name:         "arch",
			distribution: "arch",
			recommended:  "zram-generator",
			want:         []string{"zram-generator"},
		},
		{
			name:         "alpine",
			distribution: "alpine",
			recommended:  "zram-init",
			want:         []string{"zram-init"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := zramPackageCandidates(tt.distribution, tt.recommended)
			if len(got) != len(tt.want) {
				t.Fatalf("zramPackageCandidates() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("zramPackageCandidates() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestZramBackendForPackage(t *testing.T) {
	tests := map[string]string{
		"systemd-zram-generator":  zramBackendGenerator,
		"zram-generator":          zramBackendGenerator,
		"zram-generator-defaults": zramBackendGenerator,
		"zram-config":             zramBackendConfig,
		"zram-tools":              zramBackendTools,
		"zram-init":               zramBackendInit,
		"unrelated":               "",
	}
	for packageName, want := range tests {
		if got := zramBackendForPackage(packageName); got != want {
			t.Fatalf("zramBackendForPackage(%q) = %q, want %q", packageName, got, want)
		}
	}
}
