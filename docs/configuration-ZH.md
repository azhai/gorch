# 配置说明

## 文件格式

TOML 格式。默认路径：当前目录下的 `gorch.toml`。

使用 `-c /path/to/config.toml` 指定其他路径。

## 全局配置

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `LOG_DIR` | — | 默认日志目录；未指定 `STDOUT`/`STDERR` 的服务将使用 `<LOG_DIR>/<名称>.out.log` 和 `<LOG_DIR>/<名称>.err.log` |

## Web 界面配置

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `WEB_ENABLE` | `false` | 启用 Web 管理界面 |
| `WEB_ADDR` | `127.0.0.1:8080` | 监听地址 |
| `WEB_AUTH` | `false` | 启用登录认证 |
| `WEB_USER` | — | 登录用户名 |
| `WEB_PASS` | — | 登录密码 |
| `URL_PREFIX` | `""` | 子路径挂载（如 `"/gorch"`）。无需重新编译，运行时注入。 |
| `TOTP_ENABLE` | `false` | 启用 TOTP 两阶段认证 |
| `TOTP_SECRET` | — | 32 字节十六进制主密钥（64 个字符）。用 `openssl rand -hex 32` 生成 |
| `TOTP_DB` | `auth_totp.db` | TOTP 数据的 SQLite 数据库路径 |

## 服务配置字段

| 字段 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `EXEC_CMD` | 是 | — | 要执行的命令 |
| `RESTART_CMD` | 否 | — | 优雅重载命令，替代 stop+start（如 `nginx -s reload`） |
| `WORK_DIR` | 否 | 配置文件所在目录 | 进程工作目录 |
| `RESTART_POLICY` | 否 | `never` | `always`（总是重启）/ `on-failure`（失败时重启）/ `never`（不重启） |
| `BACK_OFF` | 否 | `0`（自动重启时使用 `2`） | 重启前等待秒数 |
| `PRE_ACTION` | 否 | — | 启动前通过 `sh -c` 执行的 Shell 命令 |
| `CHECK_PORT` | 否 | `0` | 若设置，启动前杀掉占用该端口的进程 |
| `PID_FILE` | 否 | — | 守护进程的 PID 文件路径 |
| `STDOUT` | 否 | `LOG_DIR/<名称>.out.log` | 标准输出日志文件路径 |
| `STDERR` | 否 | `LOG_DIR/<名称>.err.log` | 标准错误日志文件路径 |
| `DEPENDS_ON` | 否 | `[]` | 依赖的服务列表（按拓扑排序启动） |
| `CRON` | 否 | — | 6 位 cron 表达式（含秒），用于定时执行 |
| `ENV_VARS` | 否 | `{}` | 传递给进程的环境变量 |

### 启动顺序

服务启动（或重启）时，gorch 按以下顺序执行：

1. **PRE_ACTION** — 若已设置，在 `WORK_DIR` 下通过 `sh -c` 执行。失败仅记录日志，不阻止启动。
2. **CHECK_PORT** — 若 `> 0`，杀掉占用该端口的进程（`lsof` + `SIGKILL`）。
3. **StartProcess** — 启动 `EXEC_CMD`。

### 通过 RESTART_CMD 优雅重载

对于支持重载信号的守护进程（如 `nginx -s reload`、`angie -s reload`），设置 `RESTART_CMD` 后，`gorch restart <名称>` 会触发优雅重载，而非杀进程再启动。

### 守护进程跟踪

Nginx/Angie 等守护进程会 fork 出 master 进程并退出原始 PID，master 随后被 init 接管（PPID=1）。gorch 通过匹配可执行文件名定位真正的 master，优先选择 PPID=1 的进程，回退到最小 PID（master 启动最早）。

## 环境变量展开

字符串字段支持 `${VAR}` 语法，加载时自动从环境变量展开：

```toml
[services.app]
EXEC_CMD = "/app/bin/start --port ${PORT}"
WORK_DIR = "${HOME}/projects/app"
```

## 运行模式

设置 `GORCH_MODE` 控制日志详细程度：

| 值 | 日志级别 | 用途 |
|----|---------|------|
| `dev` | `debug` | 排障 — 每个 tick 显示进程状态/运行时间/内存/命令 |
| `prod`（默认） | `info` | 生产环境 |

## Cron 表达式

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

## 完整示例

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
CHECK_PORT = 8000
STDOUT = "/var/log/api.stdout.log"
STDERR = "/var/log/api.stderr.log"
DEPENDS_ON = ["redis"]
CRON = "0 */30 * * * *"
ENV_VARS = { DEBUG = "true", REDIS_URL = "redis://localhost:6379/0" }

[services.redis]
EXEC_CMD = "redis-server /etc/redis/redis.conf"
RESTART_POLICY = "always"
BACK_OFF = 3
STDOUT = "/var/log/redis.log"

[services.nginx]
EXEC_CMD = "nginx -g 'daemon off;'"
RESTART_POLICY = "always"
RESTART_CMD = "nginx -s reload"
DEPENDS_ON = ["api"]
```

## 服务配置示例

常见服务的配置模板，可直接参考使用。

### Nginx / Angie

Nginx 和 Angie 采用多进程架构：master 进程管理 worker 进程。使用 `RESTART_CMD` 实现优雅重载 — master 进程 PID 保持不变，只重启 worker。

```toml
[services.angie]
EXEC_CMD = './sbin/angie -p /opt/wares/angie'
PID_FILE = '/var/run/angie.pid'
PRE_ACTION = 'ps -efww | grep -F angie | grep -v grep | cut -w -f3 | xargs kill -9'
RESTART_CMD = './sbin/angie -p /opt/wares/angie -s reload'
RESTART_POLICY = 'on-failure'
BACK_OFF = 5
WORK_DIR = '/opt/wares/angie'
STDOUT = '/var/log/gorch/angie.out.log'
STDERR = '/var/log/gorch/angie.err.log'
```

**为什么重启后 PID 不变** — 设置了 `RESTART_CMD` 时，`gorch restart` 执行的是重载命令（如 `nginx -s reload`），而不是 stop+start。master 进程从未退出，所以 PID 保持不变。worker 进程被优雅地替换，不会中断连接。

### Redis（macOS Homebrew 安装）

```toml
[services.redis]
EXEC_CMD = '/opt/homebrew/opt/redis/bin/redis-server /opt/homebrew/etc/redis.conf'
PID_FILE = '/opt/homebrew/var/run/redis.pid'
RESTART_POLICY = 'on-failure'
BACK_OFF = 5
WORK_DIR = '/opt/homebrew/var'
STDOUT = '/opt/homebrew/var/log/redis.log'
STDERR = '/opt/homebrew/var/log/redis.log'
CHECK_PORT = 6379
```

- `EXEC_CMD` 指向 Homebrew 安装的 Redis 二进制和配置文件
- `WORK_DIR` 是 Redis 数据目录（Apple Silicon 为 `/opt/homebrew/var`，Intel 为 `/usr/local/var`）
- `CHECK_PORT = 6379` 确保重启时没有残留进程占用端口

### Go 应用

```toml
[services.myapp]
EXEC_CMD = './myapp'
WORK_DIR = '/opt/sites/myapp'
RESTART_POLICY = 'on-failure'
BACK_OFF = 5
CHECK_PORT = 9000
STDOUT = '/var/log/gorch/myapp.out.log'
STDERR = '/var/log/gorch/myapp.err.log'
```

### 静态文件服务器（weblite）

```toml
[services.mine_world]
EXEC_CMD = '/opt/wares/gorch/weblite -p 2100'
WORK_DIR = '/opt/repos/mine_world'
RESTART_POLICY = 'on-failure'
BACK_OFF = 8
STDOUT = '/var/log/gorch/mine_world.out.log'
STDERR = '/var/log/gorch/mine_world.err.log'
```

### Godoc

```toml
[services.godoc]
EXEC_CMD = './bin/godoc -http=127.0.0.1:6060'
WORK_DIR = '/Users/ryan/gopath'
RESTART_POLICY = 'on-failure'
BACK_OFF = 5
CHECK_PORT = 6060
STDOUT = '/var/log/gorch/godoc.out.log'
STDERR = '/var/log/gorch/godoc.err.log'
```

### Cron 定时任务

设置了 `CRON` 的服务按计划运行，不是常驻进程。gorch 会跳过重叠执行。

```toml
[services.daily_backup]
EXEC_CMD = './backup.sh'
WORK_DIR = '/opt/backup'
CRON = '0 0 2 * * *'  # 每天凌晨 2 点
STDOUT = '/var/log/gorch/backup.out.log'
STDERR = '/var/log/gorch/backup.err.log'
```

## 重启行为

### 普通重启（stop + start）

对于大多数服务，`gorch restart <名称>` 会先停止进程，再重新启动。PID 会变化，因为创建了新的进程。

### 优雅重载（RESTART_CMD）

设置了 `RESTART_CMD` 时，`gorch restart` 执行该命令而非 stop+start。进程保持运行 — 只有 worker 或配置被重新加载。PID 不变。

这对于支持 reload 信号的守护进程（如 Nginx/Angie）非常理想：

1. Master 进程保持运行（PID 不变）
2. Worker 进程被优雅停止并替换为新进程
3. 不中断连接，无停机时间

如果即使设置了 `RESTART_CMD` 也想强制完全 stop+start，先停止再启动：

```sh
gorch stop -s nginx
gorch start -s nginx
```
