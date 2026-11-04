# gorch

轻量级进程管理工具，使用 Odin 编写，灵感来自 [Sysg](https://github.com/ra0x3/systemg) 和 [Supervisor](http://supervisord.org/)。

单二进制文件，无运行时依赖。声明式 TOML 配置。内置 Web 管理界面。

[English](./README.md)

![Gorch Web UI](./screenshot-gorch.png)

## 为什么选择 gorch？

- **Web 界面 + REST API** — 通过浏览器仪表盘管理服务，或用简洁的 JSON API 写脚本自动化。Odin 版去掉了 Go 版的 CLI 子命令，安装为系统服务改由两个 shell 脚本负责。
- **Web 登录支持 TOTP 两阶段认证** — 账号密码直接写在配置文件里，但有了 TOTP 二阶段认证，就算密码泄露了也不怕。
- **单二进制、零依赖** — 一个文件，放哪儿都能跑。
- **优雅重载支持** — 对于 Nginx/Angie 这类守护进程，`RESTART_CMD` 让主进程 PID 保持稳定，只重启 worker。

## 不适用场景

gorch 非常适合 Web 应用、API 服务、定时任务、无状态守护进程。但**不建议**用于：

- **传统大型数据库**（MySQL、PostgreSQL 等）— 频繁重启可能导致数据不一致甚至损坏数据库。请使用数据库自带的管理工具。

## 二进制文件

本仓库只构建一个二进制文件：**`gorch`** — 进程管理工具，管理常驻服务与定时任务，内置 Web 界面和 REST API。

由 Odin 编写。Web 界面压缩后嵌入二进制，部署只需一个文件；配置旁若存在 `web/` 目录，则按文件优先于内嵌副本。

## 安装

### 从源码构建

需要 [Odin](https://odin-lang.org/) `dev-2026-10` 或更新版本；`make deps` 拉取唯一的子模块。

```sh
make deps        # 拉取 vendor/oboe
make one         # 构建本机版本 ./bin/gorch
```

发布版覆盖 macOS 与 Linux 的 arm64/amd64 四个目标（macOS 11+；Linux 为静态 musl 链接，目标机无需任何依赖）：

```sh
make build                                   # 四个目标 + bin/SHA256SUMS
make build TARGETS="linux_arm64"             # 单个目标
make build CROSS_CC="x86_64-linux-musl-gcc"  # 覆盖交叉链接器（默认 zig cc）
```

Linux 静态链接会带上 `third_party/zlib` 里的精简 zlib 源码（状态快照压缩用）；本机构建直接用系统 zlib。

### 安装为系统服务

Odin 二进制没有 `install` 子命令。两个独立脚本负责生成 systemd unit（Linux）或
launchd plist（macOS），让 gorch 前台运行并由服务管理器守护：

```sh
sudo ./scripts/gorch-install --config /etc/gorch/gorch.toml   # 系统级
./scripts/gorch-install --user --config ~/.config/gorch/gorch.toml
sudo ./scripts/gorch-uninstall                                # 卸载服务
./scripts/gorch-uninstall --user
```

`gorch-uninstall` 只删除服务文件，二进制、配置、日志和状态文件都会保留。
可先用 `--dry-run` 预览生成内容，或用 `--no-start` 只写文件不启用。全部参数及
各脚本写入的路径见[命令参考](./docs/cli-ZH.md)。macOS 下也可用
[launchd-ui](https://github.com/azu/launchd-ui) 管理生成的 agent。

## 快速上手

### 1. 创建配置文件

```sh
cp gorch.toml.example gorch.toml
```

最简配置：

```toml
[services.myapp]
EXEC_CMD = "python app.py"
```

生产环境示例：

```toml
LOG_DIR = '/tmp/gorch/logs'

[web]
WEB_ENABLE = true
WEB_AUTH = true
WEB_USER = "admin"
WEB_PASS = "secret"

[services.api]
EXEC_CMD = "python manage.py runserver 0.0.0.0:8000"
WORK_DIR = "/app/backend"
RESTART_POLICY = "on-failure"
BACK_OFF = 5
CHECK_PORT = 8000
STDOUT = "/tmp/gorch/logs/api.stdout.log"
STDERR = "/tmp/gorch/logs/api.stderr.log"
DEPENDS_ON = ["redis"]

[services.redis]
EXEC_CMD = "redis-server /etc/redis/redis.conf"
RESTART_POLICY = "always"
BACK_OFF = 3
```

### 2. 启动 gorch

```sh
gorch                          # 前台运行，自动查找配置
gorch -c /etc/gorch/gorch.toml # 显式指定配置
```

没有守护进程参数：gorch 一直前台运行，收到 `SIGTERM`/`SIGINT` 时会先停掉所有
子进程。需要后台常驻请用上面的 `gorch-install` 安装为服务。

### 3. 查看状态

打开 Web 仪表盘，或查询 API：

```sh
curl -u admin:secret http://127.0.0.1:8080/api/services
```

### 4. 控制服务

用仪表盘上的 启动 / 停止 / 重启 按钮，或调用同样的 API：

```sh
curl -u admin:secret -X POST http://127.0.0.1:8080/api/services/api/restart
curl -u admin:secret -X POST http://127.0.0.1:8080/api/services/api/stop
```

### 5. 查看日志

Web 界面提供实时的 stdout / stderr 日志查看器；API 返回 JSON 格式的尾部内容：

```sh
curl -u admin:secret 'http://127.0.0.1:8080/api/services/api/logs?type=stderr&lines=100'
```

只有 `WEB_AUTH = true` 时才需要 `-u` 账号密码。配置了 `URL_PREFIX = "/gorch"`，
所有 API 路径都要加上前缀。

### 6. Web 管理界面

当 `WEB_ENABLE = true` 时，浏览器打开 `http://127.0.0.1:8080`。

功能：
- 仪表盘：实时状态展示
- 日志查看器：stdout / stderr 切换
- 配置编辑器：两步保存（应用到内存 → 持久化到文件）
- Cron 表达式验证

## 运行时文件

gorch 启动时会创建 `/tmp/gorch` 及其 `logs/` 子目录。除非用 `LOG_DIR` 指向别处，
它写入的文件都在这里：

| 路径 | 用途 |
|------|------|
| `<LOG_DIR>/<名称>.out.log` | 服务标准输出（未设置 `STDOUT` 时的默认值） |
| `<LOG_DIR>/<名称>.err.log` | 服务标准错误（未设置 `STDERR` 时的默认值） |
| `<LOG_DIR>/gorch.state` | 服务状态的原子快照（oboe cache） |

`LOG_DIR` 默认为 `/tmp/gorch/logs`；未设置 `LOG_DIR` 时状态文件为
`/tmp/gorch/gorch.state`。`STDOUT` / `STDERR` 可覆盖单个服务的日志路径。

Odin 版没有 Unix socket、supervisor PID 文件，也没有 CLI 与 supervisor 的 IPC——
服务控制走 Web 界面和 REST API。全局 `PID_FILE`、`SERVICES_LOCK` 两个键仍会被
接受（兼容旧配置），但不再使用。

> **注意：** `/tmp` 在系统重启后会被清空（macOS 还会定期清理），因此日志不会保留。如需长期保存，请把 `LOG_DIR` 指向持久化目录。

## 文档

- [配置说明](./docs/configuration-ZH.md) — 所有配置字段、cron 表达式、环境变量、服务配置示例
- [命令参考](./docs/cli-ZH.md) — 二进制参数、服务脚本、REST API
- [Web 界面](./docs/web-ui-ZH.md) — Web 功能、子目录部署
- [TOTP 两阶段认证](./docs/totp-ZH.md) — 2FA 设置方法、推荐 APP
- [架构设计](./docs/architecture-ZH.md) — 系统设计和技术栈

## 许可证

MIT
