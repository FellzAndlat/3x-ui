package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	telemtWebBootUnitName = "3x-ui-telemt-web.service"
	telemtWebBootUnitPath = "/etc/systemd/system/3x-ui-telemt-web.service"
)

type telemtConfigSnapshot struct {
	data   []byte
	exists bool
	mode   os.FileMode
	active bool
}

type WebProxyEnableSnapshot struct {
	state        TelemtWebProxyState
	stateExisted bool
	config       telemtConfigSnapshot
}

func captureTelemtConfigSnapshot() (telemtConfigSnapshot, error) {
	snapshot := telemtConfigSnapshot{
		mode:   0o600,
		active: systemctl("is-active", "--quiet", telemtServiceName) == nil,
	}
	info, err := os.Stat(telemtConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	}
	if err != nil {
		return telemtConfigSnapshot{}, err
	}
	data, err := os.ReadFile(telemtConfigPath)
	if err != nil {
		return telemtConfigSnapshot{}, err
	}
	snapshot.data = data
	snapshot.exists = true
	snapshot.mode = info.Mode().Perm()
	return snapshot, nil
}

func restoreTelemtConfigSnapshot(snapshot telemtConfigSnapshot) error {
	if snapshot.exists {
		if current, err := os.ReadFile(telemtConfigPath); err == nil && bytes.Equal(current, snapshot.data) {
			return nil
		}
	} else if _, err := os.Stat(telemtConfigPath); errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if snapshot.exists {
		if err := os.MkdirAll(filepath.Dir(telemtConfigPath), 0o700); err != nil {
			return err
		}
		mode := snapshot.mode
		if mode == 0 {
			mode = 0o600
		}
		if err := os.WriteFile(telemtConfigPath, snapshot.data, mode); err != nil {
			return err
		}
	} else if err := os.Remove(telemtConfigPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	_ = systemctl("daemon-reload")
	if snapshot.active {
		if err := systemctl("restart", telemtServiceName); err != nil {
			return fmt.Errorf("restore Telemt service: %w", err)
		}
		return nil
	}
	if systemctl("is-active", "--quiet", telemtServiceName) == nil {
		if err := systemctl("stop", telemtServiceName); err != nil {
			return fmt.Errorf("restore stopped Telemt service: %w", err)
		}
	}
	return nil
}

func withTelemtRollbackError(operationErr, rollbackErr error) error {
	if rollbackErr == nil {
		return operationErr
	}
	return fmt.Errorf("%w; configuration rollback failed: %v", operationErr, rollbackErr)
}

func (s TelemtService) SaveConfigAtomic(c TelemtConfig) error {
	snapshot, err := captureTelemtConfigSnapshot()
	if err != nil {
		return fmt.Errorf("telemt: capture configuration before save: %w", err)
	}
	if err := s.SaveConfig(c); err != nil {
		return withTelemtRollbackError(err, restoreTelemtConfigSnapshot(snapshot))
	}
	return nil
}

func (s TelemtService) CreateProxyAtomic(req TelemtCreateRequest) (TelemtProxy, error) {
	snapshot, err := captureTelemtConfigSnapshot()
	if err != nil {
		return TelemtProxy{}, fmt.Errorf("telemt: capture configuration before proxy creation: %w", err)
	}
	proxy, err := s.CreateProxy(req)
	if err != nil {
		return TelemtProxy{}, withTelemtRollbackError(err, restoreTelemtConfigSnapshot(snapshot))
	}
	return proxy, nil
}

func (TelemtService) CaptureWebProxyEnableSnapshot() (WebProxyEnableSnapshot, error) {
	state, err := readTelemtWebState()
	if err != nil {
		return WebProxyEnableSnapshot{}, err
	}
	config, err := captureTelemtConfigSnapshot()
	if err != nil {
		return WebProxyEnableSnapshot{}, err
	}
	_, statErr := os.Stat(telemtWebStatePath)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return WebProxyEnableSnapshot{}, statErr
	}
	return WebProxyEnableSnapshot{
		state:        state,
		stateExisted: statErr == nil,
		config:       config,
	}, nil
}

func (TelemtService) WebProxyNginxInstalled() bool {
	return telemtWebCommandExists("nginx")
}

func (TelemtService) WebProxyTelemtActive() bool {
	return systemctl("is-active", "--quiet", telemtServiceName) == nil
}

func ensureWebProxyBootUnit() error {
	unit := `[Unit]
Description=3X-UI Telemt WEB Proxy dependencies
Requires=nginx.service telemt.service
After=network-online.target nginx.service telemt.service
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/bin/true
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	if err := os.WriteFile(telemtWebBootUnitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("telemt: write WEB Proxy boot unit: %w", err)
	}
	if err := systemctl("daemon-reload"); err != nil {
		_ = os.Remove(telemtWebBootUnitPath)
		return fmt.Errorf("telemt: reload WEB Proxy boot unit: %w", err)
	}
	if err := systemctl("enable", telemtWebBootUnitName); err != nil {
		_ = os.Remove(telemtWebBootUnitPath)
		_ = systemctl("daemon-reload")
		return fmt.Errorf("telemt: enable WEB Proxy boot unit: %w", err)
	}
	return nil
}

func cleanupWebProxyBootUnit() error {
	_ = systemctl("disable", "--now", telemtWebBootUnitName)
	if err := os.Remove(telemtWebBootUnitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("telemt: remove WEB Proxy boot unit: %w", err)
	}
	if err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("telemt: reload systemd after WEB Proxy boot cleanup: %w", err)
	}
	return nil
}

func (TelemtService) EnsureWebProxyBackend() error {
	state, err := readTelemtWebState()
	if err != nil {
		return err
	}
	if !state.Enabled {
		return errors.New("telemt: WEB Proxy is not configured")
	}
	if err := ensureWebProxyBootUnit(); err != nil {
		return err
	}
	_ = systemctl("reset-failed", telemtServiceName)
	if err := systemctl("start", telemtServiceName); err != nil {
		_ = cleanupWebProxyBootUnit()
		return fmt.Errorf("telemt: failed to start WEB Proxy backend: %w%s", err, telemtBackendServiceState())
	}

	addr := net.JoinHostPort(telemtWebListenIP, strconv.Itoa(state.ListenPort))
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for {
		if systemctl("is-active", "--quiet", telemtServiceName) == nil {
			conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return nil
			}
			lastErr = err
		} else {
			lastErr = errors.New("service is not active")
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = cleanupWebProxyBootUnit()
	return fmt.Errorf("telemt: WEB Proxy backend %s is not ready: %w%s; see journalctl -u telemt.service -n 50 --no-pager", addr, lastErr, telemtBackendServiceState())
}

func telemtBackendServiceState() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "systemctl", "show", telemtServiceName,
		"-p", "Result", "-p", "ExecMainStatus", "-p", "SubState", "--no-pager").Output()
	if err != nil {
		return ""
	}
	return "; " + strings.Join(strings.Fields(string(output)), " ")
}

func (TelemtService) RestoreFailedWebProxyEnable(snapshot WebProxyEnableSnapshot) error {
	return restoreTelemtConfigSnapshot(snapshot.config)
}

func (s TelemtService) RestoreWebProxyEnableSnapshot(snapshot WebProxyEnableSnapshot) error {
	var rollbackErr error
	if err := s.DisableWebProxy(); err != nil {
		rollbackErr = fmt.Errorf("disable failed WEB Proxy: %w", err)
	}

	if snapshot.stateExisted {
		if err := writeTelemtWebState(snapshot.state); err != nil && rollbackErr == nil {
			rollbackErr = fmt.Errorf("restore WEB Proxy state: %w", err)
		}
	} else if err := clearTelemtWebState(); err != nil && rollbackErr == nil {
		rollbackErr = fmt.Errorf("clear WEB Proxy state: %w", err)
	}

	if err := restoreTelemtConfigSnapshot(snapshot.config); err != nil && rollbackErr == nil {
		rollbackErr = fmt.Errorf("restore Telemt configuration: %w", err)
	}

	if snapshot.state.Enabled {
		if err := writeTelemtWebNginxConfig(snapshot.state); err != nil && rollbackErr == nil {
			rollbackErr = fmt.Errorf("restore WEB Proxy nginx config: %w", err)
		}
		if err := telemtWebEnsureNginxRunning(); err != nil && rollbackErr == nil {
			rollbackErr = fmt.Errorf("restore nginx: %w", err)
		}
		if err := telemtWebReloadNginx(); err != nil && rollbackErr == nil {
			rollbackErr = fmt.Errorf("reload restored nginx: %w", err)
		}
		if err := ensureWebProxyBootUnit(); err != nil && rollbackErr == nil {
			rollbackErr = err
		}
	} else if err := cleanupWebProxyBootUnit(); err != nil && rollbackErr == nil {
		rollbackErr = err
	}
	return rollbackErr
}

func (TelemtService) CleanupWebProxyBackendDependency() error {
	return cleanupWebProxyBootUnit()
}

func (TelemtService) CleanupFailedWebProxyEnable() {
	state, err := readTelemtWebState()
	if err != nil || state.Enabled {
		return
	}
	_ = cleanupWebProxyBootUnit()
	_ = removeTelemtWebNginxConfig()
	removeTelemtWebAcmeConfig()
	if telemtWebCommandExists("nginx") {
		_ = telemtWebStopNginx(true)
	}
}
