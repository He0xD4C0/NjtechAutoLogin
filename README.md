# NjtechAutoLogin

南京工业大学 Dr.COM 校园网自动登录与保活工具。

> 认证协议来自已经验证过的 v1 实现。v2 的重点是可靠的前台运行、服务监督和跨平台安装。

## 支持范围

| 平台 | 前台运行 | 服务安装 | 服务管理器 |
|---|---:|---:|---|
| OpenWrt | ✓ | ✓ | procd |
| Alpine Linux | ✓ | ✓ | OpenRC / supervise-daemon |
| 普通 Linux | ✓ | ✓ | systemd |
| macOS | ✓ | ✓ | 用户 LaunchAgent |
| FreeBSD / OpenBSD / NetBSD | ✓ | — | 请使用系统自己的服务包装 |

程序不会自行 fork 或伪造守护进程。`njtechlogin run` 始终在前台运行，由 procd、OpenRC、systemd 或 launchd 负责脱离终端、状态跟踪和异常重启。

## 编译

编译需要 Go 1.24 或更高版本；生成的静态 Linux 二进制运行时不需要 Go 或 glibc。

```sh
cd src
CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=2.0.0-dev" -o njtechlogin .
```

常用交叉编译示例：

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o njtechlogin-linux-arm64 .
GOOS=linux GOARCH=mipsle GOMIPS=softfloat CGO_ENABLED=0 go build -o njtechlogin-linux-mipsle .
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o njtechlogin-darwin-arm64 .
```

## 使用

### 前台运行

直接交互输入账号、密码和运营商：

```sh
njtechlogin run
```

非交互运行应通过标准输入传递密码：

```sh
printf '%s\n' '*' | njtechlogin run \
  --username '*' \
  --provider telecom \
  --password-stdin
```

`'*'` 只是文档占位符。引号不可省略，否则 shell 可能将星号展开成当前目录文件名。

默认日志写到 stdout/stderr。需要应用自行按日轮转并保留 25 天时使用：

```sh
njtechlogin run --config /etc/njtechlogin/config.yml --log-dir /var/log/njtechlogin
```

### 安装服务

下载与当前系统架构相符的二进制后运行：

```sh
njtechlogin install
```

安装器会检测当前正在使用的服务管理器：

- OpenWrt 使用 procd，并将日志交给 logd；
- Alpine/OpenRC 使用 supervise-daemon，并将输出交给 syslog；
- systemd 使用 journal；
- macOS 安装当前用户的 LaunchAgent，日志保存在 `~/Library/Logs/NjtechAutoLogin/`。

升级 v1 或替换已有安装：

```sh
njtechlogin install --force
```

安装会在写文件前完成环境和配置检查。启动验证失败时，会恢复原二进制、配置和服务定义。

### 服务管理

```sh
njtechlogin status
njtechlogin start
njtechlogin stop
njtechlogin restart
```

`status` 的退出码为：运行中 `0`，未运行或未安装 `3`，查询失败 `1`。

卸载默认保留配置和日志：

```sh
njtechlogin uninstall
```

明确删除配置与日志：

```sh
njtechlogin uninstall --purge
```

## 配置

Linux 默认配置路径为 `/etc/njtechlogin/config.yml`。macOS 用户服务默认使用 `~/Library/Application Support/NjtechAutoLogin/config.yml`。

```yaml
username: '*'
password: '*'
provider: telecom
```

配置文件会以 `0600` 权限原子写入。v1 的 `interface` 和 `log_file` 字段仍能读取，但已经弃用并会被忽略。

`--pwd` 为临时兼容入口，会让密码暴露在 shell 历史及进程列表中；使用时程序会发出警告。请改用交互输入、`--password-stdin` 或配置文件。

## v1 命令迁移

v2 使用标准子命令：

| v1 | v2 |
|---|---|
| `--install` | `install` |
| `--uninstall` | `uninstall` |
| `--start` | `start` 或前台运行的 `run` |
| `--stop` | `stop` |
| `--show` | `status` |
| `--daemon` | 已删除，由服务管理器启动 `run` |

旧形式不会继续执行，只会输出迁移提示。

## 验证边界

- 单元测试固定了 v1 的认证请求路径、查询参数和请求头。
- CI 验证 Alpine/OpenRC 的安装、脱离终端、异常重启、启停和卸载。
- OpenWrt 当前只有 procd/rootfs 模拟验证；`v2.0.0` 稳定版发布前仍需真机验收。
- 当前开发环境不在校园网内，因此不以真实登录结果作为 v2 守护重构的验收项。

## 许可证

Apache-2.0，详见 [LICENSE](LICENSE)。
