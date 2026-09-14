package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type statusTestManager struct {
	Manager
	state serviceState
	err   error
}

func (m statusTestManager) Name() string                  { return "test" }
func (m statusTestManager) Status() (serviceState, error) { return m.state, m.err }

func TestLegacyCommandShowsMigration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"--show"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "njtechlogin status") {
		t.Fatalf("missing migration hint: %s", stderr.String())
	}
}

func TestPasswordFlagsAreMutuallyExclusive(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI([]string{
		"run", "--username", "*", "--provider", "telecom",
		"--pwd", "*", "--password-stdin",
	}, strings.NewReader("*\n"), &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "不能同时使用") {
		t.Fatalf("unexpected error: %s", stderr.String())
	}
}

func TestDeprecatedPasswordWarns(t *testing.T) {
	var stderr bytes.Buffer
	cfg, err := resolveCredentials(credentialOptions{
		configPath: t.TempDir() + "/missing.yml",
		username:   "*",
		password:   "*",
		provider:   "cmcc",
	}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "*" || !strings.Contains(stderr.String(), "已弃用") {
		t.Fatalf("cfg=%+v warning=%q", cfg, stderr.String())
	}
}

func TestPasswordStdinOnlyStripsLineEnding(t *testing.T) {
	got, err := readPasswordLine(strings.NewReader("  pass word  \r\nignored"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "  pass word  " {
		t.Fatalf("password = %q", got)
	}
}

func TestHelpContainsOnlyPlaceholderCredentials(t *testing.T) {
	var output bytes.Buffer
	printHelp(&output)
	text := output.String()
	if !strings.Contains(text, "--username '*'") || !strings.Contains(text, "--password-stdin") {
		t.Fatalf("help missing safe example: %s", text)
	}
}

func TestStatusExitCodes(t *testing.T) {
	tests := []struct {
		name  string
		state serviceState
		err   error
		want  int
	}{
		{name: "running", state: serviceRunning, want: exitOK},
		{name: "stopped", state: serviceStopped, want: exitNotAlive},
		{name: "failure", state: serviceUnknown, err: errors.New("query failed"), want: exitFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			manager := statusTestManager{state: test.state, err: test.err}
			if got := statusExitCode(manager, &stdout, &stderr); got != test.want {
				t.Fatalf("status exit = %d, want %d", got, test.want)
			}
		})
	}
}

func TestServiceInstallRejectsRelativeConfigPath(t *testing.T) {
	dir := t.TempDir()
	manager := &rollbackManager{layout: serviceLayout{
		BinaryPath: filepath.Join(dir, "bin"), ServicePath: filepath.Join(dir, "service"),
		ConfigPath: filepath.Join(dir, "config.yml"), StatePath: filepath.Join(dir, "state.json"),
	}}
	self := filepath.Join(dir, "self")
	if err := os.WriteFile(self, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	err := installManagedService(manager, self, "relative.yml", Config{
		Username: "*", Password: "*", Provider: "telecom",
	}, false)
	if err == nil || !strings.Contains(err.Error(), "绝对路径") {
		t.Fatalf("unexpected error: %v", err)
	}
}
