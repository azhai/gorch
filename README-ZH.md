# gorch

轻量级进程管理工具，使用 Go 编写，灵感来自 [Supervisor](http://supervisord.org/)。

单二进制文件，无运行时依赖。声明式 TOML 配置。内置 Web 管理界面。

[English](./README.md)

![Gorch Web UI](./screenshot-gorch.png)

## 安装

### 从源码构建

```sh
export PATH="$GOPATH/bin:$PATH"
go install github.com/azhai/gorch@latest
```

### 安装为系统服务

macOS 下建议使用 [launchd-ui](https://github.com/azu/launchd-ui) 管理服务。

```sh
gorch install            # 系统级安装（Linux: systemd, macOS: launchd）
gorch install --user     # 用户级安装
gorch uninstall          # 卸载
```

`gorch install` 会自动写入服务文件、加载并启动。如果自动启动失败，可手动加载：

```sh
# macOS:
launchctl load -w ~/Library/LaunchAgents/com.github.azhai.gorch.plist
# Linux:
systemctl daemon-reload
systemctl enable --now gorch
```

## 快速上手

```sh
# 1. 创建配置文件
cp gorch.toml.example gorch.toml
# 编辑 gorch.toml，定义你的服务

# 2. 启动
gorch start                    # 前台运行，默认配置
gorch start -c /etc/gorch.toml # 指定配置文件路径
gorch start -d                 # 以守护进程方式运行

# 3. 查看状态
gorch status
gorch status -s api            # 查看单个服务
gorch status --json            # JSON 格式输出

# 4. 控制服务
gorch restart -s api
gorch stop

# 5. 查看日志
gorch logs -s api              # 最近 100 行
gorch logs -s api -n 500      # 最近 500 行
```

## 配置

配置文件为 TOML 格式，默认读取当前目录下的 `gorch.toml`。

### 最简示例

```toml
[services.myapp]
EXEC_CMD = "python app.py"
```

### 完整示例

```toml
LOG_DIR = "/var/log/gorch"

[web]
WEB_ENABLE = true
WEB_ADDR = "127.0.0.1:8080"
WEB_AUTH = true
WEB_USER = "admin"
WEB_PASS = "secret"

[services.api]
EXEC_CMD = "python manage.py runserver 0.0.0.0:8000"
WORK_DIR = "/app/backend"
RESTART_POLICY = "on-failure"
BACK_OFF = 5
STDOUT = "/var/log/api.stdout.log"
STDERR = "/var/log/api.stderr.log"
DEPENDS_ON = ["postgres"]
CRON = "0 */30 * * * *"
ENV_VARS = { DEBUG = "true", DATABASE_URL = "postgres://user:pass@localhost:5432/db" }

[services.postgres]
EXEC_CMD = "postgres -D /var/lib/postgres"
RESTART_POLICY = "always"
BACK_OFF = 3
STDOUT = "/var/log/postgres.log"

[services.nginx]
EXEC_CMD = "nginx -g 'daemon off;'"
RESTART_POLICY = "always"
DEPENDS_ON = ["api"]
```

### 服务配置字段

| 字段 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `EXEC_CMD` | 是 | — | 要执行的命令 |
| `WORK_DIR` | 否 | 配置文件所在目录 | 进程工作目录 |
| `RESTART_POLICY` | 否 | `never` | `always`（总是重启）/ `on-failure`（失败时重启）/ `never`（不重启） |
| `BACK_OFF` | 否 | `0` | 重启前等待秒数 |
| `STDOUT` | 否 | `LOG_DIR/<名称>.out.log` | 标准输出日志文件路径 |
| `STDERR` | 否 | `LOG_DIR/<名称>.err.log` | 标准错误日志文件路径 |
| `DEPENDS_ON` | 否 | `[]` | 依赖的服务列表（按拓扑排序启动） |
| `CRON` | 否 | — | 6 位 cron 表达式（含秒），用于定时执行 |
| `ENV_VARS` | 否 | `{}` | 传递给进程的环境变量 |

### Web 界面配置

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `WEB_ENABLE` | `false` | 启用 Web 管理界面 |
| `WEB_ADDR` | `127.0.0.1:8080` | 监听地址 |
| `WEB_AUTH` | `false` | 启用登录认证 |
| `WEB_USER` | — | 登录用户名 |
| `WEB_PASS` | — | 登录密码 |

### 全局配置

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `LOG_DIR` | — | 默认日志目录；未指定 STDOUT/STDERR 的服务将使用 `<LOG_DIR>/<名称>.out.log` 和 `<LOG_DIR>/<名称>.err.log` |

### 环境变量展开

字符串字段支持 `${VAR}` 语法，加载时自动从环境变量展开：

```toml
[services.app]
EXEC_CMD = "/app/bin/start --port ${PORT}"
WORK_DIR = "${HOME}/projects/app"
```

### Cron 表达式

6 位格式，含秒：

```
┌──────── 秒 (0-59)
│ ┌────── 分 (0-59)
│ │ ┌──── 时 (0-23)
│ │ │ ┌── 日 (1-31)
│ │ │ │ ┌─ 月 (1-12)
│ │ │ │ │ ┌ 星期 (0-6, 周日=0)
│ │ │ │ │ │
* * * * * *
```

示例：`0 */30 * * * *`（每 30 分钟），`0 0 8 * * 1-5`（工作日早 8 点）

Cron 服务不能手动启动，按计划自动执行。重叠执行会被检测并跳过。

## Web 管理界面

`WEB_ENABLE = true` 时，访问 `http://<WEB_ADDR>` 即可使用。

功能：
- **仪表盘** — 实时服务状态，WebSocket 推送更新，支持启动/停止/重启操作
- **日志** — 查看 stdout 和 stderr 日志，支持标签页切换
- **配置** — 编辑服务配置，两步保存：应用（内存）→ 保存到文件（持久化）
- **Cron 验证** — 验证 cron 表达式，预览下次执行时间

## 命令参考

| 命令 | 说明 |
|------|------|
| `gorch start [-c 配置] [-d]` | 启动服务（`-d` 以守护进程运行） |
| `gorch stop` | 停止所有服务 |
| `gorch restart -s <名称>` | 重启指定服务 |
| `gorch status [-s 名称] [-j]` | 查看状态（`-j` 输出 JSON） |
| `gorch logs -s <名称> [-n 行数]` | 查看服务日志 |
| `gorch install [--user]` | 安装为系统服务 |
| `gorch uninstall [--user]` | 卸载系统服务 |

## 架构

```
CLI ──► Unix Socket IPC ──► Supervisor ──► Fiber Web Server
                                   │
                                   ├── Process Manager
                                   ├── Cron Scheduler
                                   ├── Log Manager
                                   └── Status Cache
```

## 技术栈

- **Go** — 核心运行时
- **Cobra** — CLI 框架
- **Fiber** — HTTP 服务器
- **robfig/cron** — Cron 调度
- **go-toml/v2** — TOML 解析
- **React + TypeScript + Tailwind** — Web 界面（通过 embed 嵌入）

## 许可证

MIT
