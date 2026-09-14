package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/term"
)

const (
	exitOK       = 0
	exitFailure  = 1
	exitUsage    = 2
	exitNotAlive = 3
)

type credentialOptions struct {
	configPath        string
	username          string
	password          string
	provider          string
	passwordFromStdin bool
	logDir            string
}

func runCLI(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		printHelp(out)
		return exitOK
	}
	if strings.HasPrefix(args[0], "--") {
		return printLegacyMigration(args[0], errOut)
	}

	switch args[0] {
	case "help", "-h":
		printHelp(out)
		return exitOK
	case "version":
		fmt.Fprintf(out, "%s %s\n", programName, version)
		return exitOK
	case "run":
		return runCommandCLI(args[1:], in, out, errOut)
	case "install":
		return installCommandCLI(args[1:], in, out, errOut)
	case "uninstall":
		return uninstallCommandCLI(args[1:], out, errOut)
	case "start", "stop", "restart", "status":
		return serviceCommandCLI(args[0], args[1:], out, errOut)
	default:
		fmt.Fprintf(errOut, "未知命令 %q\n", args[0])
		printHelp(errOut)
		return exitUsage
	}
}

func printHelp(out io.Writer) {
	fmt.Fprintf(out, `%s %s
南京工业大学校园网自动登录与保活工具

用法:
  %s run [--config PATH] [--username USER] [--provider ISP]
               [--password-stdin | --pwd VALUE] [--log-dir DIR]
  %s install [--config PATH] [--username USER] [--provider ISP]
                   [--password-stdin | --pwd VALUE] [--force]
  %s uninstall [--purge]
  %s start|stop|restart|status
  %s version

示例（* 仅为占位符）:
  %s run --username '*' --provider telecom --password-stdin
  %s install --username '*' --provider cmcc --password-stdin

--pwd 已弃用，因为密码会进入 shell 历史和进程列表；请使用交互输入、
--password-stdin 或权限为 0600 的配置文件。
`, programName, version, programName, programName, programName, programName, programName, programName, programName)
}

func printLegacyMigration(command string, errOut io.Writer) int {
	legacy := map[string]string{
		"--install": "install", "--uninstall": "uninstall", "--start": "start",
		"--stop": "stop", "--show": "status", "--daemon": "run", "--help": "help",
	}
	if replacement, ok := legacy[command]; ok {
		fmt.Fprintf(errOut, "%s 是 v1 调用方式；v2 请使用: %s %s\n", command, programName, replacement)
	} else {
		fmt.Fprintf(errOut, "未知的旧式参数 %s；请使用 %s help\n", command, programName)
	}
	return exitUsage
}

func addCredentialFlags(fs *flag.FlagSet, options *credentialOptions, defaultConfig string, withLog bool) {
	fs.StringVar(&options.configPath, "config", defaultConfig, "配置文件路径")
	fs.StringVar(&options.username, "username", "", "校园网账号")
	fs.StringVar(&options.password, "pwd", "", "密码（已弃用）")
	fs.StringVar(&options.provider, "provider", "", "运营商: telecom/cmcc")
	fs.BoolVar(&options.passwordFromStdin, "password-stdin", false, "从标准输入读取密码")
	if withLog {
		fs.StringVar(&options.logDir, "log-dir", "", "按日轮转日志目录")
	}
}

func runCommandCLI(args []string, in io.Reader, out, errOut io.Writer) int {
	options := credentialOptions{}
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(errOut)
	addCredentialFlags(fs, &options, defaultRunConfigPath(), true)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(errOut, "run 不接受位置参数")
		return exitUsage
	}
	cfg, err := resolveCredentials(options, in, out, errOut)
	if err != nil {
		fmt.Fprintf(errOut, "配置错误: %v\n", err)
		return exitUsage
	}

	var logger *Logger
	if options.logDir == "" {
		logger = NewStreamLogger(out)
	} else {
		logger, err = NewFileLogger(options.logDir)
		if err != nil {
			fmt.Fprintf(errOut, "初始化日志失败: %v\n", err)
			return exitFailure
		}
	}
	globalLogger = logger
	defer logger.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	globalLogger.Printf("前台服务启动，PID=%d", os.Getpid())
	monitorLoop(ctx, cfg)
	globalLogger.Println("程序退出")
	return exitOK
}

func installCommandCLI(args []string, in io.Reader, out, errOut io.Writer) int {
	manager, err := detectServiceManager(productionProbe())
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFailure
	}
	options := credentialOptions{}
	force := false
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(errOut)
	addCredentialFlags(fs, &options, manager.Layout().ConfigPath, false)
	fs.BoolVar(&force, "force", false, "升级或替换现有安装")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(errOut, "install 不接受位置参数")
		return exitUsage
	}
	if err := manager.Preflight(); err != nil {
		fmt.Fprintln(errOut, err)
		return exitFailure
	}
	cfg, err := resolveCredentials(options, in, out, errOut)
	if err != nil {
		fmt.Fprintf(errOut, "配置错误: %v\n", err)
		return exitUsage
	}
	selfPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(errOut, "无法确定当前可执行文件: %v\n", err)
		return exitFailure
	}
	if resolved, resolveErr := filepath.EvalSymlinks(selfPath); resolveErr == nil && resolved != "" {
		selfPath = resolved
	}
	if err := installManagedService(manager, selfPath, options.configPath, cfg, force); err != nil {
		fmt.Fprintf(errOut, "安装失败: %v\n", err)
		return exitFailure
	}
	fmt.Fprintf(out, "安装完成：%s 服务正在运行\n", manager.Name())
	fmt.Fprintf(out, "配置文件：%s\n", options.configPath)
	return exitOK
}

func uninstallCommandCLI(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(errOut)
	purge := false
	fs.BoolVar(&purge, "purge", false, "同时删除配置与日志")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(errOut, "uninstall 不接受位置参数")
		return exitUsage
	}
	manager, err := detectServiceManager(productionProbe())
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFailure
	}
	if err := manager.Preflight(); err != nil {
		fmt.Fprintln(errOut, err)
		return exitFailure
	}
	if err := uninstallManagedService(manager, purge); err != nil {
		fmt.Fprintf(errOut, "卸载失败: %v\n", err)
		return exitFailure
	}
	if purge {
		fmt.Fprintln(out, "服务、配置和日志已删除")
	} else {
		fmt.Fprintln(out, "服务已卸载，配置和日志已保留")
	}
	return exitOK
}

func serviceCommandCLI(command string, args []string, out, errOut io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(errOut, "%s 不接受参数\n", command)
		return exitUsage
	}
	manager, err := detectServiceManager(productionProbe())
	if err != nil {
		if command == "status" {
			fmt.Fprintf(errOut, "状态查询失败: %v\n", err)
			return exitFailure
		}
		fmt.Fprintln(errOut, err)
		return exitFailure
	}
	if command == "status" {
		return statusExitCode(manager, out, errOut)
	}
	if err := manager.Preflight(); err != nil {
		fmt.Fprintln(errOut, err)
		return exitFailure
	}
	if !manager.Installed() {
		fmt.Fprintln(errOut, "服务尚未安装")
		return exitFailure
	}
	switch command {
	case "start":
		if manager.Name() == "launchd" {
			if err := manager.Enable(); err != nil {
				fmt.Fprintf(errOut, "启用服务失败: %v\n", err)
				return exitFailure
			}
		}
		err = manager.Start()
	case "stop":
		if manager.Name() == "launchd" {
			if err := manager.Disable(); err != nil {
				fmt.Fprintf(errOut, "禁用服务失败: %v\n", err)
				return exitFailure
			}
		}
		err = manager.Stop()
	case "restart":
		err = manager.Restart()
	}
	if err != nil {
		fmt.Fprintf(errOut, "%s 失败: %v\n", command, err)
		return exitFailure
	}
	fmt.Fprintf(out, "%s 完成\n", command)
	return exitOK
}

func statusExitCode(manager Manager, out, errOut io.Writer) int {
	state, err := manager.Status()
	if err != nil {
		fmt.Fprintf(errOut, "状态查询失败: %v\n", err)
		return exitFailure
	}
	if state == serviceRunning {
		fmt.Fprintf(out, "状态：%s 服务正在运行\n", manager.Name())
		return exitOK
	}
	fmt.Fprintf(out, "状态：%s 服务未运行或未安装\n", manager.Name())
	return exitNotAlive
}

func resolveCredentials(options credentialOptions, in io.Reader, out, errOut io.Writer) (Config, error) {
	if options.passwordFromStdin && options.password != "" {
		return Config{}, errors.New("--password-stdin 与 --pwd 不能同时使用")
	}
	cfg := Config{}
	if loaded, warnings, err := loadConfig(options.configPath); err == nil {
		cfg = loaded
		for _, warning := range warnings {
			fmt.Fprintf(errOut, "警告: %s\n", warning)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	if options.username != "" {
		cfg.Username = options.username
	}
	if options.provider != "" {
		cfg.Provider = options.provider
	}
	if options.password != "" {
		fmt.Fprintln(errOut, "警告: --pwd 已弃用，密码可能暴露在 shell 历史和进程列表中；请使用 --password-stdin")
		cfg.Password = options.password
	}
	if options.passwordFromStdin {
		password, err := readPasswordLine(in)
		if err != nil {
			return Config{}, fmt.Errorf("从标准输入读取密码失败: %w", err)
		}
		cfg.Password = password
	}

	if isTerminal(in) {
		reader := bufio.NewReader(in)
		if cfg.Username == "" {
			fmt.Fprint(out, "请输入校园网账号: ")
			value, err := reader.ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return Config{}, err
			}
			cfg.Username = strings.TrimSpace(value)
		}
		if cfg.Password == "" {
			fmt.Fprint(out, "请输入密码: ")
			file := in.(*os.File)
			value, err := term.ReadPassword(int(file.Fd()))
			fmt.Fprintln(out)
			if err != nil {
				return Config{}, err
			}
			cfg.Password = string(value)
		}
		if cfg.Provider == "" {
			fmt.Fprint(out, "请输入运营商 (telecom/cmcc): ")
			value, err := reader.ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return Config{}, err
			}
			cfg.Provider = strings.TrimSpace(value)
		}
	}
	return cfg, validateConfig(cfg)
}

func readPasswordLine(in io.Reader) (string, error) {
	reader := bufio.NewReader(in)
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	value = strings.TrimSuffix(value, "\n")
	value = strings.TrimSuffix(value, "\r")
	return value, nil
}

func isTerminal(in io.Reader) bool {
	file, ok := in.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
