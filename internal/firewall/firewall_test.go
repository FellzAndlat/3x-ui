package firewall

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeCommandResult struct {
	output []byte
	err    error
}

type fakeRunCall struct {
	path string
	args []string
}

type fakeCommandRunner struct {
	paths   map[string]string
	outputs map[string]fakeCommandResult
	runs    []fakeRunCall
	runErr  error
	onRun   func(*fakeCommandRunner, string, []string)
}

func (f *fakeCommandRunner) LookPath(file string) (string, error) {
	if path, ok := f.paths[file]; ok {
		return path, nil
	}
	return "", errors.New("not found")
}

func (f *fakeCommandRunner) CombinedOutput(_ context.Context, path string, args ...string) ([]byte, error) {
	result, ok := f.outputs[commandKey(path, args...)]
	if !ok {
		return nil, errors.New("command result not configured")
	}
	return result.output, result.err
}

func (f *fakeCommandRunner) Run(_ context.Context, path string, args ...string) error {
	copiedArgs := append([]string(nil), args...)
	f.runs = append(f.runs, fakeRunCall{path: path, args: copiedArgs})
	if f.runErr != nil {
		return f.runErr
	}
	if f.onRun != nil {
		f.onRun(f, path, copiedArgs)
	}
	return nil
}

func commandKey(path string, args ...string) string {
	return path + " " + strings.Join(args, " ")
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name    string
		paths   map[string]string
		outputs map[string]fakeCommandResult
		want    Status
	}{
		{
			name:  "active ufw",
			paths: map[string]string{"ufw": "/usr/sbin/ufw"},
			outputs: map[string]fakeCommandResult{
				"/usr/sbin/ufw status": {output: []byte("Status: active\n")},
			},
			want: Status{Backend: BackendUFW, Installed: true, Active: true},
		},
		{
			name: "later active firewalld beats inactive ufw",
			paths: map[string]string{
				"ufw":          "/usr/sbin/ufw",
				"firewall-cmd": "/usr/bin/firewall-cmd",
			},
			outputs: map[string]fakeCommandResult{
				"/usr/sbin/ufw status":          {output: []byte("Status: inactive\n")},
				"/usr/bin/firewall-cmd --state": {output: []byte("running\n")},
			},
			want: Status{Backend: BackendFirewalld, Installed: true, Active: true},
		},
		{
			name:    "installed inactive ufw",
			paths:   map[string]string{"ufw": "/usr/sbin/ufw"},
			outputs: map[string]fakeCommandResult{"/usr/sbin/ufw status": {output: []byte("Status: inactive\n")}},
			want:    Status{Backend: BackendUFW, Installed: true},
		},
		{
			name:    "active nftables",
			paths:   map[string]string{"nft": "/usr/sbin/nft"},
			outputs: map[string]fakeCommandResult{"/usr/sbin/nft list ruleset": {output: []byte("table inet filter {}\n")}},
			want:    Status{Backend: BackendNftables, Installed: true, Active: true},
		},
		{
			name:    "active iptables",
			paths:   map[string]string{"iptables": "/usr/sbin/iptables"},
			outputs: map[string]fakeCommandResult{"/usr/sbin/iptables -S": {output: []byte("-P INPUT DROP\n-P FORWARD ACCEPT\n")}},
			want:    Status{Backend: BackendIptables, Installed: true, Active: true},
		},
		{
			name: "no supported firewall",
			want: Status{Backend: BackendNone},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeCommandRunner{paths: tt.paths, outputs: tt.outputs}
			if runner.paths == nil {
				runner.paths = map[string]string{}
			}
			if runner.outputs == nil {
				runner.outputs = map[string]fakeCommandResult{}
			}

			got := detect(context.Background(), runner)
			if got != tt.want {
				t.Fatalf("detect() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestInstallUFWPackageManagers(t *testing.T) {
	tests := []struct {
		name     string
		binary   string
		path     string
		wantArgs []string
	}{
		{name: "apt-get", binary: "apt-get", path: "/usr/bin/apt-get", wantArgs: []string{"install", "-y", "ufw"}},
		{name: "dnf", binary: "dnf", path: "/usr/bin/dnf", wantArgs: []string{"install", "-y", "ufw"}},
		{name: "yum", binary: "yum", path: "/usr/bin/yum", wantArgs: []string{"install", "-y", "ufw"}},
		{name: "pacman", binary: "pacman", path: "/usr/bin/pacman", wantArgs: []string{"-S", "--needed", "--noconfirm", "ufw"}},
		{name: "zypper", binary: "zypper", path: "/usr/bin/zypper", wantArgs: []string{"--non-interactive", "install", "ufw"}},
		{name: "apk", binary: "apk", path: "/sbin/apk", wantArgs: []string{"add", "ufw"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeCommandRunner{
				paths:   map[string]string{tt.binary: tt.path},
				outputs: map[string]fakeCommandResult{},
			}
			runner.onRun = func(f *fakeCommandRunner, _ string, _ []string) {
				f.paths["ufw"] = "/usr/sbin/ufw"
				f.outputs["/usr/sbin/ufw status"] = fakeCommandResult{output: []byte("Status: inactive\n")}
			}

			status, err := installUFW(context.Background(), runner)
			if err != nil {
				t.Fatalf("installUFW() error = %v", err)
			}
			if status != (Status{Backend: BackendUFW, Installed: true}) {
				t.Fatalf("installUFW() status = %#v", status)
			}
			if len(runner.runs) != 1 {
				t.Fatalf("Run calls = %d, want 1", len(runner.runs))
			}
			call := runner.runs[0]
			if call.path != tt.path || !reflect.DeepEqual(call.args, tt.wantArgs) {
				t.Fatalf("Run() = %q %v, want %q %v", call.path, call.args, tt.path, tt.wantArgs)
			}
		})
	}
}

func TestInstallUFWAlreadyInstalled(t *testing.T) {
	runner := &fakeCommandRunner{
		paths: map[string]string{"ufw": "/usr/sbin/ufw"},
		outputs: map[string]fakeCommandResult{
			"/usr/sbin/ufw status": {output: []byte("Status: active\n")},
		},
	}

	status, err := installUFW(context.Background(), runner)
	if err != nil {
		t.Fatalf("installUFW() error = %v", err)
	}
	if status != (Status{Backend: BackendUFW, Installed: true, Active: true}) {
		t.Fatalf("installUFW() status = %#v", status)
	}
	if len(runner.runs) != 0 {
		t.Fatalf("Run calls = %d, want 0", len(runner.runs))
	}
}

func TestInstallUFWNoPackageManager(t *testing.T) {
	runner := &fakeCommandRunner{paths: map[string]string{}, outputs: map[string]fakeCommandResult{}}
	_, err := installUFW(context.Background(), runner)
	if !errors.Is(err, errNoPackageManager) {
		t.Fatalf("installUFW() error = %v, want %v", err, errNoPackageManager)
	}
}

func TestInstallUFWDoesNotEnableFirewall(t *testing.T) {
	runner := &fakeCommandRunner{
		paths:   map[string]string{"apt-get": "/usr/bin/apt-get"},
		outputs: map[string]fakeCommandResult{},
	}
	runner.onRun = func(f *fakeCommandRunner, _ string, _ []string) {
		f.paths["ufw"] = "/usr/sbin/ufw"
		f.outputs["/usr/sbin/ufw status"] = fakeCommandResult{output: []byte("Status: inactive\n")}
	}

	if _, err := installUFW(context.Background(), runner); err != nil {
		t.Fatalf("installUFW() error = %v", err)
	}
	for _, call := range runner.runs {
		if strings.HasSuffix(call.path, "/ufw") && reflect.DeepEqual(call.args, []string{"enable"}) {
			t.Fatal("installUFW() must not enable ufw")
		}
	}
}

func TestInstallUFWVerifiesExecutable(t *testing.T) {
	runner := &fakeCommandRunner{
		paths:   map[string]string{"apt-get": "/usr/bin/apt-get"},
		outputs: map[string]fakeCommandResult{},
	}

	_, err := installUFW(context.Background(), runner)
	if err == nil || !strings.Contains(err.Error(), "ufw executable was not found") {
		t.Fatalf("installUFW() error = %v", err)
	}
}
