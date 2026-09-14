package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicConfigWriteForcesPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yml")
	if err := atomicWriteFile(path, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(path, []byte("new-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("mode = %o", got)
	}
}

func TestLegacyConfigLoadsWithWarnings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	data := []byte("username: '*'\npassword: '*'\nprovider: telecom\ninterface: eth0\nlog_file: /tmp/old.log\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, warnings, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Username != "*" || cfg.Password != "*" || cfg.Provider != "telecom" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestMarshalConfigDropsLegacyFields(t *testing.T) {
	data, err := marshalConfig(Config{Username: "*", Password: "*", Provider: "cmcc"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "interface") || strings.Contains(text, "log_file") {
		t.Fatalf("legacy fields remained: %s", text)
	}
}
