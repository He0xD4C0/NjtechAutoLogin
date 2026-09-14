package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	programName = "njtechlogin"
	serviceName = "njtechlogin"
	launchLabel = "io.github.he0xd4c0.njtechlogin"
)

type serviceState int

const (
	serviceUnknown serviceState = iota
	serviceStopped
	serviceRunning
)

type serviceLayout struct {
	BinaryPath  string
	ConfigPath  string
	ServicePath string
	StatePath   string
	LogDir      string
}

type installRecord struct {
	Manager    string `json:"manager"`
	BinaryPath string `json:"binary_path"`
	ConfigPath string `json:"config_path"`
	LogDir     string `json:"log_dir,omitempty"`
}

type serviceSpec struct {
	BinaryPath string
	ConfigPath string
	LogDir     string
}

type Manager interface {
	Name() string
	Layout() serviceLayout
	Preflight() error
	Definition(serviceSpec) ([]byte, os.FileMode, error)
	Installed() bool
	Enable() error
	Disable() error
	Start() error
	Stop() error
	Restart() error
	Status() (serviceState, error)
	Reload() error
}

type commandExecutor interface {
	Run(name string, args ...string) ([]byte, error)
}

type realExecutor struct{}

func (realExecutor) Run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, message)
	}
	return out, nil
}

type systemProbe struct {
	goos     string
	exists   func(string) bool
	lookPath func(string) (string, error)
	homeDir  func() (string, error)
	executor commandExecutor
}

func productionProbe() systemProbe {
	return systemProbe{
		goos: runtime.GOOS,
		exists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
		lookPath: exec.LookPath,
		homeDir:  os.UserHomeDir,
		executor: realExecutor{},
	}
}

func detectServiceManager(probe systemProbe) (Manager, error) {
	switch probe.goos {
	case "darwin":
		home, err := probe.homeDir()
		if err != nil {
			return nil, fmt.Errorf("无法确定用户主目录: %w", err)
		}
		if _, err := probe.lookPath("launchctl"); err != nil {
			return nil, errors.New("当前 macOS 环境缺少 launchctl")
		}
		return newLaunchdManager(home, probe.executor), nil
	case "linux":
		if probe.exists("/etc/openwrt_release") && probe.exists("/etc/rc.common") {
			return newProcdManager(probe.executor), nil
		}
		if probe.exists("/run/systemd/system") {
			if _, err := probe.lookPath("systemctl"); err == nil {
				return newSystemdManager(probe.executor), nil
			}
		}
		if probe.exists("/sbin/openrc-run") || probe.exists("/run/openrc") {
			if _, err := probe.lookPath("rc-service"); err == nil {
				if _, err := probe.lookPath("rc-update"); err == nil {
					return newOpenRCManager(probe.executor), nil
				}
			}
		}
		return nil, errors.New("未检测到受支持的服务管理器（procd、OpenRC 或 systemd）；请使用 njtechlogin run")
	default:
		return nil, fmt.Errorf("%s 暂不支持服务安装；请使用 njtechlogin run", probe.goos)
	}
}

type fileSnapshot struct {
	path   string
	exists bool
	data   []byte
	mode   os.FileMode
}

func snapshotFile(path string) (fileSnapshot, error) {
	snapshot := fileSnapshot{path: path}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	if !info.Mode().IsRegular() {
		return snapshot, fmt.Errorf("目标不是普通文件: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	snapshot.exists = true
	snapshot.data = data
	snapshot.mode = info.Mode().Perm()
	return snapshot, nil
}

func restoreSnapshots(snapshots []fileSnapshot) error {
	var restoreErr error
	for index := len(snapshots) - 1; index >= 0; index-- {
		snapshot := snapshots[index]
		if snapshot.exists {
			if err := atomicWriteFile(snapshot.path, snapshot.data, snapshot.mode); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		} else if err := os.Remove(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			restoreErr = errors.Join(restoreErr, err)
		}
	}
	return restoreErr
}

func installManagedService(manager Manager, selfPath, configPath string, cfg Config, force bool) error {
	if err := manager.Preflight(); err != nil {
		return err
	}
	layout := manager.Layout()
	if configPath == "" {
		configPath = layout.ConfigPath
	}
	if !filepath.IsAbs(configPath) {
		return errors.New("服务配置路径必须是绝对路径")
	}
	if !force && (manager.Installed() || fileExists(layout.BinaryPath)) {
		return errors.New("服务或目标二进制已存在；确认升级时请使用 --force")
	}

	binaryData, err := os.ReadFile(selfPath)
	if err != nil {
		return fmt.Errorf("读取当前可执行文件失败: %w", err)
	}
	configData, err := marshalConfig(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	spec := serviceSpec{BinaryPath: layout.BinaryPath, ConfigPath: configPath, LogDir: layout.LogDir}
	definition, definitionMode, err := manager.Definition(spec)
	if err != nil {
		return err
	}
	recordData, err := json.MarshalIndent(installRecord{
		Manager: manager.Name(), BinaryPath: layout.BinaryPath, ConfigPath: configPath, LogDir: layout.LogDir,
	}, "", "  ")
	if err != nil {
		return err
	}
	recordData = append(recordData, '\n')

	targets := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{configPath, configData, 0600},
		{layout.BinaryPath, binaryData, 0755},
		{layout.ServicePath, definition, definitionMode},
		{layout.StatePath, recordData, 0600},
	}
	snapshots := make([]fileSnapshot, 0, len(targets))
	for _, target := range targets {
		snapshot, err := snapshotFile(target.path)
		if err != nil {
			return fmt.Errorf("安装预检失败: %w", err)
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := preflightTargetDirectories(targets); err != nil {
		return fmt.Errorf("安装目录预检失败: %w", err)
	}

	previouslyRunning, statusErr := manager.Status()
	if statusErr != nil && !force {
		return fmt.Errorf("无法确定原服务运行状态: %w", statusErr)
	}
	if manager.Installed() {
		if isLegacyInitScript(layout.ServicePath) {
			if err := stopLegacyPID(layout.BinaryPath); err != nil {
				return fmt.Errorf("停止 v1 守护进程失败: %w", err)
			}
		} else if err := manager.Stop(); err != nil {
			return fmt.Errorf("停止原服务失败: %w", err)
		}
	}
	if err := stopLegacyPID(layout.BinaryPath); err != nil {
		return fmt.Errorf("停止 v1 守护进程失败: %w", err)
	}

	rollback := func(cause error) error {
		var rollbackErr error
		rollbackErr = errors.Join(rollbackErr, manager.Stop())
		rollbackErr = errors.Join(rollbackErr, manager.Disable())
		rollbackErr = errors.Join(rollbackErr, restoreSnapshots(snapshots))
		rollbackErr = errors.Join(rollbackErr, manager.Reload())
		if previouslyRunning == serviceRunning {
			rollbackErr = errors.Join(rollbackErr, manager.Enable())
			rollbackErr = errors.Join(rollbackErr, manager.Start())
		}
		if rollbackErr != nil {
			return fmt.Errorf("%w；回滚未完全成功: %v", cause, rollbackErr)
		}
		return cause
	}

	for _, target := range targets {
		if err := atomicWriteFile(target.path, target.data, target.mode); err != nil {
			return rollback(fmt.Errorf("写入 %s 失败: %w", target.path, err))
		}
	}
	if err := manager.Reload(); err != nil {
		return rollback(fmt.Errorf("刷新服务管理器失败: %w", err))
	}
	if err := manager.Enable(); err != nil {
		return rollback(fmt.Errorf("启用服务失败: %w", err))
	}
	if err := manager.Start(); err != nil {
		return rollback(fmt.Errorf("启动服务失败: %w", err))
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, statusErr := manager.Status()
		if statusErr == nil && state == serviceRunning {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return rollback(errors.New("服务启动后未进入运行状态"))
}

func preflightTargetDirectories(targets []struct {
	path string
	data []byte
	mode os.FileMode
}) error {
	checked := make(map[string]bool)
	for _, target := range targets {
		dir := filepath.Dir(target.path)
		for {
			info, err := os.Stat(dir)
			if err == nil {
				if !info.IsDir() {
					return fmt.Errorf("%s 不是目录", dir)
				}
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				return fmt.Errorf("找不到 %s 的现有父目录", target.path)
			}
			dir = parent
		}
		if checked[dir] {
			continue
		}
		probe, err := os.CreateTemp(dir, ".njtechlogin-preflight-*")
		if err != nil {
			return fmt.Errorf("%s 不可写: %w", dir, err)
		}
		probePath := probe.Name()
		closeErr := probe.Close()
		removeErr := os.Remove(probePath)
		if closeErr != nil {
			return closeErr
		}
		if removeErr != nil {
			return removeErr
		}
		checked[dir] = true
	}
	return nil
}

func uninstallManagedService(manager Manager, purge bool) error {
	layout := manager.Layout()
	record := installRecord{
		Manager: manager.Name(), BinaryPath: layout.BinaryPath, ConfigPath: layout.ConfigPath, LogDir: layout.LogDir,
	}
	if data, err := os.ReadFile(layout.StatePath); err == nil {
		var saved installRecord
		if json.Unmarshal(data, &saved) == nil && saved.Manager == manager.Name() && filepath.IsAbs(saved.ConfigPath) {
			record.ConfigPath = saved.ConfigPath
		}
	}
	if manager.Installed() {
		if err := manager.Stop(); err != nil {
			return fmt.Errorf("停止服务失败: %w", err)
		}
		if err := manager.Disable(); err != nil {
			return fmt.Errorf("禁用服务失败: %w", err)
		}
	}
	if err := os.Remove(layout.ServicePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := manager.Reload(); err != nil {
		return err
	}
	for _, path := range []string{record.BinaryPath, layout.StatePath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if manager.Name() == "launchd" {
		_ = os.Remove(filepath.Dir(record.BinaryPath))
	}
	if purge {
		if err := os.Remove(record.ConfigPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if record.LogDir != "" {
			if err := os.RemoveAll(record.LogDir); err != nil {
				return err
			}
		}
		_ = os.Remove(filepath.Dir(layout.StatePath))
	}
	return nil
}

func isLegacyInitScript(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(data), "USE_PROCD=0")
}

func stopLegacyPID(expectedBinary string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	data, err := os.ReadFile("/var/run/njtechlogin.pid")
	if err != nil {
		return nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return nil
	}
	executable, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil || filepath.Base(executable) != filepath.Base(expectedBinary) {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	for attempt := 0; attempt < 20; attempt++ {
		if process.Signal(syscall.Signal(0)) != nil {
			_ = os.Remove("/var/run/njtechlogin.pid")
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("旧守护进程未在 5 秒内退出")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
