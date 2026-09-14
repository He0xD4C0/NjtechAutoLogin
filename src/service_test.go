package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeExecutor struct {
	calls []string
}

func (f *fakeExecutor) Run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(append([]string{name}, args...), " "))
	return nil, nil
}

func TestDetectServiceManagerPriority(t *testing.T) {
	executor := &fakeExecutor{}
	existing := map[string]bool{
		"/etc/openwrt_release": true,
		"/etc/rc.common":       true,
		"/run/systemd/system":  true,
		"/sbin/openrc-run":     true,
	}
	probe := systemProbe{
		goos:     "linux",
		exists:   func(path string) bool { return existing[path] },
		lookPath: func(name string) (string, error) { return "/sbin/" + name, nil },
		homeDir:  func() (string, error) { return "/home/test", nil },
		executor: executor,
	}
	manager, err := detectServiceManager(probe)
	if err != nil {
		t.Fatal(err)
	}
	if manager.Name() != "procd" {
		t.Fatalf("manager = %s", manager.Name())
	}
}

func TestDetectServiceManagerRejectsUnsupportedSystem(t *testing.T) {
	_, err := detectServiceManager(systemProbe{goos: "freebsd"})
	if err == nil || !strings.Contains(err.Error(), "njtechlogin run") {
		t.Fatalf("unexpected detection result: %v", err)
	}
}

func TestManagerDefinitionsRunForeground(t *testing.T) {
	spec := serviceSpec{BinaryPath: "/usr/local/bin/njtechlogin", ConfigPath: "/etc/njtechlogin/config.yml", LogDir: "/tmp/logs"}
	executor := &fakeExecutor{}
	cases := []struct {
		name     string
		manager  Manager
		contains []string
	}{
		{"procd", newProcdManager(executor), []string{"USE_PROCD=1", " run --config ", "procd_set_param respawn 60 5 5"}},
		{"openrc", newOpenRCManager(executor), []string{"#!/sbin/openrc-run", `supervisor="supervise-daemon"`, "respawn_delay=5"}},
		{"systemd", newSystemdManager(executor), []string{"Type=simple", " run --config ", "RestartSec=5s"}},
		{"launchd", newLaunchdManager("/Users/test", executor), []string{"<key>KeepAlive</key><true/>", "<key>ThrottleInterval</key><integer>5</integer>", "--log-dir"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			definition, _, err := test.manager.Definition(spec)
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range test.contains {
				if !strings.Contains(string(definition), fragment) {
					t.Errorf("definition missing %q:\n%s", fragment, definition)
				}
			}
		})
	}
}

func TestSystemdUnitPassesSystemdAnalyze(t *testing.T) {
	tool, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze is unavailable")
	}
	manager := newSystemdManager(&fakeExecutor{})
	definition, _, err := manager.Definition(serviceSpec{
		BinaryPath: "/bin/true", ConfigPath: "/tmp/config.yml",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "njtechlogin.service")
	if err := os.WriteFile(path, definition, 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(tool, "verify", path).CombinedOutput(); err != nil {
		t.Fatalf("systemd-analyze verify: %v\n%s", err, output)
	}
}

func TestLaunchdPlistPassesPlutil(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("plutil validation only runs on macOS")
	}
	tool, err := exec.LookPath("plutil")
	if err != nil {
		t.Skip("plutil is unavailable")
	}
	manager := newLaunchdManager("/Users/test", &fakeExecutor{})
	definition, _, err := manager.Definition(serviceSpec{
		BinaryPath: "/tmp/njtechlogin", ConfigPath: "/tmp/config.yml", LogDir: "/tmp/logs",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "njtechlogin.plist")
	if err := os.WriteFile(path, definition, 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(tool, "-lint", path).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %v\n%s", err, output)
	}
}

type rollbackManager struct {
	layout     serviceLayout
	startCalls int
}

func (m *rollbackManager) Name() string          { return "fake" }
func (m *rollbackManager) Layout() serviceLayout { return m.layout }
func (m *rollbackManager) Preflight() error      { return nil }
func (m *rollbackManager) Definition(serviceSpec) ([]byte, os.FileMode, error) {
	return []byte("new-service"), 0644, nil
}
func (m *rollbackManager) Installed() bool               { return fileExists(m.layout.ServicePath) }
func (m *rollbackManager) Enable() error                 { return nil }
func (m *rollbackManager) Disable() error                { return nil }
func (m *rollbackManager) Start() error                  { m.startCalls++; return errors.New("start failed") }
func (m *rollbackManager) Stop() error                   { return nil }
func (m *rollbackManager) Restart() error                { return nil }
func (m *rollbackManager) Status() (serviceState, error) { return serviceRunning, nil }
func (m *rollbackManager) Reload() error                 { return nil }

func TestInstallFailureRestoresAllFiles(t *testing.T) {
	dir := t.TempDir()
	manager := &rollbackManager{layout: serviceLayout{
		BinaryPath:  filepath.Join(dir, "bin"),
		ConfigPath:  filepath.Join(dir, "config.yml"),
		ServicePath: filepath.Join(dir, "service"),
		StatePath:   filepath.Join(dir, "state.json"),
	}}
	old := map[string]string{
		manager.layout.BinaryPath:  "old-binary",
		manager.layout.ConfigPath:  "old-config",
		manager.layout.ServicePath: "old-service",
		manager.layout.StatePath:   "old-state",
	}
	for path, value := range old {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	self := filepath.Join(dir, "self")
	if err := os.WriteFile(self, []byte("new-binary"), 0755); err != nil {
		t.Fatal(err)
	}
	err := installManagedService(manager, self, manager.layout.ConfigPath, Config{
		Username: "*", Password: "*", Provider: "telecom",
	}, true)
	if err == nil {
		t.Fatal("expected install failure")
	}
	for path, expected := range old {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(data) != expected {
			t.Errorf("%s = %q, want %q", path, data, expected)
		}
	}
	if manager.startCalls != 2 {
		t.Fatalf("start calls = %d, want new start plus rollback restart", manager.startCalls)
	}
}

func TestRecognizesLegacySelfDaemonInitScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "njtechlogin")
	if err := os.WriteFile(path, []byte("#!/bin/sh /etc/rc.common\nUSE_PROCD=0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if !isLegacyInitScript(path) {
		t.Fatal("legacy USE_PROCD=0 script was not recognized")
	}
	if isLegacyInitScript(filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("missing script was recognized as legacy")
	}
}

type uninstallTestManager struct {
	layout   serviceLayout
	stopped  bool
	disabled bool
}

func (m *uninstallTestManager) Name() string          { return "test" }
func (m *uninstallTestManager) Layout() serviceLayout { return m.layout }
func (m *uninstallTestManager) Preflight() error      { return nil }
func (m *uninstallTestManager) Definition(serviceSpec) ([]byte, os.FileMode, error) {
	return nil, 0, nil
}
func (m *uninstallTestManager) Installed() bool { return fileExists(m.layout.ServicePath) }
func (m *uninstallTestManager) Enable() error   { return nil }
func (m *uninstallTestManager) Disable() error  { m.disabled = true; return nil }
func (m *uninstallTestManager) Start() error    { return nil }
func (m *uninstallTestManager) Stop() error     { m.stopped = true; return nil }
func (m *uninstallTestManager) Restart() error  { return nil }
func (m *uninstallTestManager) Status() (serviceState, error) {
	return serviceStopped, nil
}
func (m *uninstallTestManager) Reload() error { return nil }

func TestUninstallPreservesOrPurgesUserData(t *testing.T) {
	for _, purge := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserve", true: "purge"}[purge], func(t *testing.T) {
			root := t.TempDir()
			manager := &uninstallTestManager{layout: serviceLayout{
				BinaryPath:  filepath.Join(root, "bin", "njtechlogin"),
				ConfigPath:  filepath.Join(root, "etc", "config.yml"),
				ServicePath: filepath.Join(root, "init", "njtechlogin"),
				StatePath:   filepath.Join(root, "etc", "install.json"),
				LogDir:      filepath.Join(root, "logs"),
			}}
			record, err := json.Marshal(installRecord{
				Manager: "test", BinaryPath: manager.layout.BinaryPath,
				ConfigPath: manager.layout.ConfigPath, LogDir: manager.layout.LogDir,
			})
			if err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{
				manager.layout.BinaryPath:                           []byte("binary"),
				manager.layout.ConfigPath:                           []byte("config"),
				manager.layout.ServicePath:                          []byte("service"),
				manager.layout.StatePath:                            record,
				filepath.Join(manager.layout.LogDir, "current.log"): []byte("log"),
			}
			for path, data := range files {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := uninstallManagedService(manager, purge); err != nil {
				t.Fatal(err)
			}
			if !manager.stopped || !manager.disabled {
				t.Fatal("installed service was not stopped and disabled")
			}
			for _, path := range []string{manager.layout.BinaryPath, manager.layout.ServicePath, manager.layout.StatePath} {
				if fileExists(path) {
					t.Fatalf("managed file remains: %s", path)
				}
			}
			for _, path := range []string{manager.layout.ConfigPath, manager.layout.LogDir} {
				if fileExists(path) == purge {
					t.Fatalf("user data path %s has wrong existence for purge=%v", path, purge)
				}
			}
		})
	}
}
