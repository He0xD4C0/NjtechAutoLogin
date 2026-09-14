package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func requireRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("Linux 服务安装与管理需要 root 权限")
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type procdManager struct {
	layout   serviceLayout
	executor commandExecutor
}

func newProcdManager(executor commandExecutor) *procdManager {
	return &procdManager{
		layout: serviceLayout{
			BinaryPath: "/usr/bin/njtechlogin", ConfigPath: "/etc/njtechlogin/config.yml",
			ServicePath: "/etc/init.d/njtechlogin", StatePath: "/etc/njtechlogin/install.json",
		},
		executor: executor,
	}
}

func (m *procdManager) Name() string          { return "procd" }
func (m *procdManager) Layout() serviceLayout { return m.layout }
func (m *procdManager) Installed() bool       { return fileExists(m.layout.ServicePath) }
func (m *procdManager) Preflight() error {
	if err := requireRoot(); err != nil {
		return err
	}
	if !fileExists("/etc/rc.common") {
		return errors.New("缺少 /etc/rc.common，当前环境不是可用的 OpenWrt/procd 系统")
	}
	return nil
}
func (m *procdManager) Definition(spec serviceSpec) ([]byte, os.FileMode, error) {
	script := `#!/bin/sh /etc/rc.common

USE_PROCD=1
START=99
STOP=10

start_service() {
	procd_open_instance
	procd_set_param command ` + shellQuote(spec.BinaryPath) + ` run --config ` + shellQuote(spec.ConfigPath) + `
	procd_set_param respawn 60 5 5
	procd_set_param stdout 1
	procd_set_param stderr 1
	procd_close_instance
}
`
	return []byte(script), 0755, nil
}
func (m *procdManager) Enable() error {
	_, err := m.executor.Run(m.layout.ServicePath, "enable")
	return err
}
func (m *procdManager) Disable() error {
	_, err := m.executor.Run(m.layout.ServicePath, "disable")
	return err
}
func (m *procdManager) Start() error {
	_, err := m.executor.Run(m.layout.ServicePath, "start")
	return err
}
func (m *procdManager) Stop() error {
	if !m.Installed() {
		return nil
	}
	_, err := m.executor.Run(m.layout.ServicePath, "stop")
	return err
}
func (m *procdManager) Restart() error {
	_, err := m.executor.Run(m.layout.ServicePath, "restart")
	return err
}
func (m *procdManager) Status() (serviceState, error) {
	if !m.Installed() {
		return serviceStopped, nil
	}
	if _, err := m.executor.Run(m.layout.ServicePath, "running"); err != nil {
		if commandExitCode(err) == 1 {
			return serviceStopped, nil
		}
		return serviceUnknown, err
	}
	return serviceRunning, nil
}
func (m *procdManager) Reload() error { return nil }

type openRCManager struct {
	layout   serviceLayout
	executor commandExecutor
}

func newOpenRCManager(executor commandExecutor) *openRCManager {
	return &openRCManager{
		layout: serviceLayout{
			BinaryPath: "/usr/local/bin/njtechlogin", ConfigPath: "/etc/njtechlogin/config.yml",
			ServicePath: "/etc/init.d/njtechlogin", StatePath: "/etc/njtechlogin/install.json",
		},
		executor: executor,
	}
}

func (m *openRCManager) Name() string          { return "openrc" }
func (m *openRCManager) Layout() serviceLayout { return m.layout }
func (m *openRCManager) Installed() bool       { return fileExists(m.layout.ServicePath) }
func (m *openRCManager) Preflight() error {
	if err := requireRoot(); err != nil {
		return err
	}
	for _, path := range []string{"/sbin/openrc-run", "/sbin/rc-service", "/sbin/rc-update", "/sbin/supervise-daemon"} {
		if !fileExists(path) {
			return fmt.Errorf("OpenRC 环境缺少 %s", path)
		}
	}
	return nil
}
func (m *openRCManager) Definition(spec serviceSpec) ([]byte, os.FileMode, error) {
	script := `#!/sbin/openrc-run

name="NjtechAutoLogin"
description="NJTech campus network login keeper"
supervisor="supervise-daemon"
command=` + shellQuote(spec.BinaryPath) + `
command_args=` + shellQuote("run --config "+spec.ConfigPath) + `
output_logger="logger -t njtechlogin"
error_logger="logger -t njtechlogin"
respawn_delay=5
respawn_max=5
respawn_period=60
retry="TERM/15/KILL/5"

depend() {
	after networking
	use logger
}
`
	return []byte(script), 0755, nil
}
func (m *openRCManager) Enable() error {
	_, err := m.executor.Run("rc-update", "add", serviceName, "default")
	return err
}
func (m *openRCManager) Disable() error {
	_, err := m.executor.Run("rc-update", "del", serviceName, "default")
	return err
}
func (m *openRCManager) Start() error {
	_, err := m.executor.Run("rc-service", serviceName, "start")
	return err
}
func (m *openRCManager) Stop() error {
	if !m.Installed() {
		return nil
	}
	_, err := m.executor.Run("rc-service", serviceName, "stop")
	return err
}
func (m *openRCManager) Restart() error {
	_, err := m.executor.Run("rc-service", serviceName, "restart")
	return err
}
func (m *openRCManager) Status() (serviceState, error) {
	if !m.Installed() {
		return serviceStopped, nil
	}
	if _, err := m.executor.Run("rc-service", serviceName, "status"); err != nil {
		if commandExitCode(err) == 3 {
			return serviceStopped, nil
		}
		return serviceUnknown, err
	}
	return serviceRunning, nil
}
func (m *openRCManager) Reload() error { return nil }

type systemdManager struct {
	layout   serviceLayout
	executor commandExecutor
}

func newSystemdManager(executor commandExecutor) *systemdManager {
	return &systemdManager{
		layout: serviceLayout{
			BinaryPath: "/usr/local/bin/njtechlogin", ConfigPath: "/etc/njtechlogin/config.yml",
			ServicePath: "/etc/systemd/system/njtechlogin.service", StatePath: "/etc/njtechlogin/install.json",
		},
		executor: executor,
	}
}

func (m *systemdManager) Name() string          { return "systemd" }
func (m *systemdManager) Layout() serviceLayout { return m.layout }
func (m *systemdManager) Installed() bool       { return fileExists(m.layout.ServicePath) }
func (m *systemdManager) Preflight() error {
	if err := requireRoot(); err != nil {
		return err
	}
	if !fileExists("/run/systemd/system") {
		return errors.New("systemd 当前不是正在运行的服务管理器")
	}
	return nil
}
func (m *systemdManager) Definition(spec serviceSpec) ([]byte, os.FileMode, error) {
	unit := `[Unit]
Description=NJTech campus network login keeper
Wants=network-online.target
After=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple
ExecStart=` + strconv.Quote(spec.BinaryPath) + ` run --config ` + strconv.Quote(spec.ConfigPath) + `
Restart=on-failure
RestartSec=5s
TimeoutStopSec=20s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true

[Install]
WantedBy=multi-user.target
`
	return []byte(unit), 0644, nil
}
func (m *systemdManager) Enable() error {
	_, err := m.executor.Run("systemctl", "enable", serviceName)
	return err
}
func (m *systemdManager) Disable() error {
	_, err := m.executor.Run("systemctl", "disable", serviceName)
	return err
}
func (m *systemdManager) Start() error {
	_, err := m.executor.Run("systemctl", "start", serviceName)
	return err
}
func (m *systemdManager) Stop() error {
	if !m.Installed() {
		return nil
	}
	_, err := m.executor.Run("systemctl", "stop", serviceName)
	return err
}
func (m *systemdManager) Restart() error {
	_, err := m.executor.Run("systemctl", "restart", serviceName)
	return err
}
func (m *systemdManager) Status() (serviceState, error) {
	if !m.Installed() {
		return serviceStopped, nil
	}
	if _, err := m.executor.Run("systemctl", "is-active", "--quiet", serviceName); err != nil {
		code := commandExitCode(err)
		if code == 3 || code == 4 {
			return serviceStopped, nil
		}
		return serviceUnknown, err
	}
	return serviceRunning, nil
}
func (m *systemdManager) Reload() error {
	_, err := m.executor.Run("systemctl", "daemon-reload")
	return err
}

type launchdManager struct {
	layout   serviceLayout
	executor commandExecutor
	uid      int
}

func newLaunchdManager(home string, executor commandExecutor) *launchdManager {
	appSupport := filepath.Join(home, "Library", "Application Support", "NjtechAutoLogin")
	return &launchdManager{
		layout: serviceLayout{
			BinaryPath:  filepath.Join(appSupport, "bin", "njtechlogin"),
			ConfigPath:  filepath.Join(appSupport, "config.yml"),
			ServicePath: filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist"),
			StatePath:   filepath.Join(appSupport, "install.json"),
			LogDir:      filepath.Join(home, "Library", "Logs", "NjtechAutoLogin"),
		},
		executor: executor,
		uid:      os.Getuid(),
	}
}

func (m *launchdManager) Name() string          { return "launchd" }
func (m *launchdManager) Layout() serviceLayout { return m.layout }
func (m *launchdManager) Installed() bool       { return fileExists(m.layout.ServicePath) }
func (m *launchdManager) domain() string        { return fmt.Sprintf("gui/%d", m.uid) }
func (m *launchdManager) target() string        { return m.domain() + "/" + launchLabel }
func (m *launchdManager) Preflight() error {
	if os.Geteuid() == 0 || os.Getenv("SUDO_USER") != "" {
		return errors.New("macOS LaunchAgent 必须由当前登录用户直接安装，不能使用 sudo/root")
	}
	if _, err := m.executor.Run("launchctl", "print", m.domain()); err != nil {
		return fmt.Errorf("当前用户没有可用的 launchd GUI 会话: %w", err)
	}
	return nil
}
func xmlValue(value string) string {
	var buffer bytes.Buffer
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}
func (m *launchdManager) Definition(spec serviceSpec) ([]byte, os.FileMode, error) {
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + launchLabel + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + xmlValue(spec.BinaryPath) + `</string>
    <string>run</string>
    <string>--config</string>
    <string>` + xmlValue(spec.ConfigPath) + `</string>
    <string>--log-dir</string>
    <string>` + xmlValue(spec.LogDir) + `</string>
  </array>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>ProcessType</key><string>Background</string>
  <key>Umask</key><integer>63</integer>
</dict>
</plist>
`
	return []byte(plist), 0644, nil
}
func (m *launchdManager) Enable() error {
	_, err := m.executor.Run("launchctl", "enable", m.target())
	return err
}
func (m *launchdManager) Disable() error {
	_, err := m.executor.Run("launchctl", "disable", m.target())
	return err
}
func (m *launchdManager) Start() error {
	_ = os.MkdirAll(m.layout.LogDir, 0700)
	if _, err := m.executor.Run("launchctl", "bootstrap", m.domain(), m.layout.ServicePath); err != nil {
		if _, kickErr := m.executor.Run("launchctl", "kickstart", "-k", m.target()); kickErr != nil {
			return err
		}
	}
	return nil
}
func (m *launchdManager) Stop() error {
	if !m.Installed() {
		return nil
	}
	_, err := m.executor.Run("launchctl", "bootout", m.target())
	if err != nil && !strings.Contains(err.Error(), "Could not find service") {
		return err
	}
	return nil
}
func (m *launchdManager) Restart() error {
	_, err := m.executor.Run("launchctl", "kickstart", "-k", m.target())
	return err
}
func (m *launchdManager) Status() (serviceState, error) {
	if !m.Installed() {
		return serviceStopped, nil
	}
	if output, err := m.executor.Run("launchctl", "print", m.target()); err != nil {
		if strings.Contains(string(output), "Could not find service") || commandExitCode(err) == 113 {
			return serviceStopped, nil
		}
		return serviceUnknown, err
	}
	return serviceRunning, nil
}
func (m *launchdManager) Reload() error { return nil }

func commandExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
