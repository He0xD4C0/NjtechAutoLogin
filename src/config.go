package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Provider string `yaml:"provider"`
}

type diskConfig struct {
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
	Provider  string `yaml:"provider"`
	Interface string `yaml:"interface,omitempty"`
	LogFile   string `yaml:"log_file,omitempty"`
}

func defaultRunConfigPath() string {
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, "Library", "Application Support", "NjtechAutoLogin", "config.yml")
		}
	}
	return "/etc/njtechlogin/config.yml"
}

func loadConfig(path string) (Config, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, nil, err
	}
	var disk diskConfig
	if err := yaml.Unmarshal(data, &disk); err != nil {
		return Config{}, nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	warnings := make([]string, 0, 2)
	if disk.Interface != "" {
		warnings = append(warnings, "配置项 interface 已弃用并将被忽略")
	}
	if disk.LogFile != "" {
		warnings = append(warnings, "配置项 log_file 已弃用，请改用 --log-dir")
	}
	cfg := Config{Username: disk.Username, Password: disk.Password, Provider: disk.Provider}
	return cfg, warnings, nil
}

func validateConfig(cfg Config) error {
	if cfg.Username == "" {
		return errors.New("缺少校园网账号")
	}
	if cfg.Password == "" {
		return errors.New("缺少校园网密码")
	}
	if cfg.Provider != "telecom" && cfg.Provider != "cmcc" {
		return errors.New("运营商必须为 telecom 或 cmcc")
	}
	return nil
}

func marshalConfig(cfg Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".njtechlogin-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return os.Chmod(path, mode)
}
