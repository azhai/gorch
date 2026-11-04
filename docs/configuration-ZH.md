# 配置说明

## 文件格式

TOML 格式。未指定 `-c` 时按以下顺序查找配置文件：

1. `$GORCH_CONFIG`
2. `./gorch.toml`
3. `~/.config/gorch/gorch.toml`
4. `/etc/gorch/gorch.toml`

也可用 `-c /path/to/config.toml` 直接指定路径。

## 全局配置

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `LOG_DIR` | `/tmp/gorch/logs` | 默认日志目录；未指定 `STDOUT`/`STDERR` 的服务使用 `<LOG_DIR>/<名称>.out.log` 和 `<LOG_DIR>/<名称>.err.log` |
| `PID_FILE` | `/tmp/gorch/gorch.pid` | 兼容字段 — 会解析并原样保存，但不会写 PID 文件 |
| `SERVICES_LOCK` | `/tmp/gorch/gorch-services.lock` | 兼容字段 — 会解析并原样保存，但不会写锁文件 |

### 兼容字段（当前不生效）

Odin 版保留这些键，是为了让已有的 `gorch.toml` 和 Web 配置编辑器能正常加载、回写，但目前没有任何运行时代码使用它们：

| 字段 | Go 版的作用 | Odin 版现状 |
|------|------------|-------------|
| `PID_FILE`（全局） | supervisor 的 PID 文件 | 不写文件 |
| `SERVICES_LOCK` | 记录服务 PID，重启后接管进程 | 不写文件；状态实际存在 oboe cache 快照 `gorch.state` 里 |
| `PID_FILE`（服务级） | 守护进程 PID 文件，用于定位真正的 master | 启动进程时不读取 |
| `CHECK_PORT` | 启动前杀掉占用 TCP 端口的进程 | 只做 0–65535 校验，不检查端口 |
| `ENV_VARS` | 传给子进程的环境变量 | 不传递；子进程直接继承 gorch 自己的环境 |

`DEPENDS_ON` 只实现了一半：加载时会校验未知依赖、自依赖和依赖环，但启动时是按**服务名字母序**遍历，不是按依赖顺序。如果确实需要顺序，请写在 `PRE_ACTION` 或启动命令里。

### 日志按天轮转

所有服务日志（`STDOUT` / `STDERR`）每天轮转一次：跨到新的一天时，当前文件会被复制为
`<路径>.<YYYY-MM-DD>`，原文件截断为空，因此 `STDOUT` 永远指向当天的日志。

采用「先复制再截断」而不是「重命名」，是为了不影响正在运行的服务：gorch 把日志文件
直接交给服务，服务在整个生命周期里持有这个 fd（即使 gorch 本身重启也不受影响），
所以文件必须留在原路径——重命名会导致服务一直往已归档的文件里写。也正因为轮转由
gorch 负责，当 gorch 没运行时产生的日志会在下次启动时按其最后写入日期切分。

`/dev/null`、命名管道等非普通文件不会被轮转。

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
| `TOTP_SECRET` | — | 32 字节主密钥，写成 64 个十六进制字符（`openssl rand -hex 32`）；不设置则 2FA 不启用 |
| `TOTP_DB` | `/var/lib/gorch/totp.db` | 存放 TOTP 绑定、备用码和已用时间窗的 **JSON 文件**路径 —— 不是 SQLite |

## 服务配置字段

| 字段 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `EXEC_CMD` | 是（设置了 `CRON` 时可不填） | — | 通过 `/bin/sh -c` 执行；普通命令自动加 `exec` 前缀；长度上限 1024 字节 |
| `RESTART_CMD` | 否 | — | 优雅重载命令，替代 stop+start（如 `nginx -s reload`）；15 秒超时 |
| `WORK_DIR` | 否 | 配置文件所在目录 | 进程工作目录 |
| `RESTART_POLICY` | 否 | `never` | `always`（总是重启）/ `on-failure`（失败时重启）/ `never`（不重启） |
| `BACK_OFF` | 否 | `0`（回退到 `5`） | 重启前等待秒数 |
| `PRE_ACTION` | 否 | — | 启动前通过 `/bin/sh -c` 执行的命令；15 秒超时，失败仅告警 |
| `CHECK_PORT` | 否 | `0` | 只校验、不生效 —— 见「兼容字段」 |
| `PID_FILE` | 否 | — | 兼容字段，不生效 |
| `STDOUT` | 否 | `LOG_DIR/<名称>.out.log` | 标准输出日志文件路径 |
| `STDERR` | 否 | `LOG_DIR/<名称>.err.log` | 标准错误日志文件路径 |
| `DEPENDS_ON` | 否 | `[]` | 会校验未知依赖 / 自依赖 / 依赖环，但不控制启动顺序 |
| `CRON` | 否 | — | 5 位或 6 位 cron 表达式，见「Cron 表达式」 |
| `CRON_TIMEOUT` | 否 | `0`（关闭） | 秒；定时任务运行超过此时长会被 SIGKILL 杀掉 |
| `ENV_VARS` | 否 | `{}` | 兼容字段，不生效 |

### 启动顺序

服务启动（或重启）时，gorch 按以下顺序执行：

1. **PRE_ACTION** — 若已设置，在 `WORK_DIR` 下通过 `/bin/sh -c` 执行，15 秒超时；失败只记录告警，不阻止启动。
2. **日志文件** — 以追加模式打开 `STDOUT` / `STDERR`，把文件描述符直接交给子进程。
3. **EXEC_CMD** — 通过 `/bin/sh -c` 启动。不含 shell 元字符的普通命令会加 `exec` 前缀，让 gorch 跟踪到的 pid 就是服务本身；命令长度上限 1024 字节。

### 通过 RESTART_CMD 优雅重载

对于支持重载信号的守护进程（如 `nginx -s reload`、`angie -s reload`），设置 `RESTART_CMD` 后，重启服务（Web 界面或 `POST /api/services/<名称>/restart`）会执行该命令，而非杀进程再启动。命令有 15 秒超时；若失败，API 返回错误，服务保持原状。

### 重启时的进程接管

gorch 跟踪的是自己启动的 pid。重启时从 `gorch.state`（oboe cache 快照）读取记录的 pid，只有当该进程仍存活**且**启动时间一致时才会接管——pid 被复用会被识别并拒绝接管，避免误杀无关进程。Odin 版**不会**按可执行文件名或 PPID=1 去扫描 fork 出来的 master，那是 Go 版的做法。

## 环境变量展开

`${VAR}` 在加载配置时展开，但**只作用于以下服务字段**：
`EXEC_CMD`、`WORK_DIR`、`RESTART_CMD`、`PRE_ACTION`、`STDOUT`、`STDERR`、`PID_FILE`。
变量未设置会导致**配置加载失败**（gorch 拒绝启动），而不是展开为空字符串。
其它字段（包括 `ENV_VARS` 里的值）不做展开。

```toml
[services.app]
EXEC_CMD = "/app/bin/start --port ${PORT}"
WORK_DIR = "${HOME}/projects/app"
```

## 运行模式

`GORCH_MODE` 控制最低日志级别：

| 值 | 日志级别 | 用途 |
|----|---------|------|
| `dev` | `debug` | 排障 —— 为 debug 输出预留 |
| 其它值（默认） | `info` | 生产环境 |

当前实现还没有任何 debug 级日志调用，所以 `dev` 与生产模式的实际输出相同。

## Cron 表达式

支持 5 位或 6 位（空格分隔）：6 位时第一位是秒，5 位时秒默认为 `0`：

```
6 位： 秒 分 时 日 月 星期
5 位：    分 时 日 月 星期

秒 0-59 | 分 0-59 | 时 0-23 | 日 1-31 | 月 1-12 | 星期 0-7（0 和 7 都表示周日）
```

示例：`0 */30 * * * *`（每 30 分钟）、`0 0 8 * * 1-5`（工作日早 8 点）、
`0 2 * * *`（每天 02:00，5 位写法）。

也支持简写：`@every 30s` / `@every 5m` / `@every 2h`，以及 `@yearly`（`@annually`）、
`@monthly`、`@weekly`、`@daily`（`@midnight`）、`@hourly`、`@minutely`。

定时任务不会在启动时拉起，由调度器按计划触发。手动 `start` 会立即额外执行一次、不影响原有节奏；
`restart` 则会终止正在执行的那次运行。重叠执行会被检测并跳过。用 `CRON_TIMEOUT`（秒）可以杀掉卡住的运行。

## 完整示例

```toml
LOG_DIR = "/tmp/gorch/logs"

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
STDOUT = "/tmp/gorch/logs/api.stdout.log"
STDERR = "/tmp/gorch/logs/api.stderr.log"

[services.redis]
EXEC_CMD = "redis-server /etc/redis/redis.conf"
RESTART_POLICY = "always"
BACK_OFF = 3
STDOUT = "/tmp/gorch/logs/redis.log"

[services.nginx]
EXEC_CMD = "nginx -g 'daemon off;'"
RESTART_POLICY = "always"
RESTART_CMD = "nginx -s reload"
```

## 服务配置示例

常见服务的配置模板，可直接参考使用。

### Nginx / Angie

Nginx 和 Angie 采用多进程架构：master 进程管理 worker 进程。使用 `RESTART_CMD` 实现优雅重载 — master 进程 PID 保持不变，只重启 worker。

```toml
[services.angie]
EXEC_CMD = './sbin/angie -p /opt/wares/angie'
PRE_ACTION = 'ps -efww | grep -F angie | grep -v grep | cut -w -f3 | xargs kill -9'
RESTART_CMD = './sbin/angie -p /opt/wares/angie -s reload'
RESTART_POLICY = 'on-failure'
BACK_OFF = 5
WORK_DIR = '/opt/wares/angie'
STDOUT = '/tmp/gorch/logs/angie.out.log'
STDERR = '/tmp/gorch/logs/angie.err.log'
```

**为什么重启后 PID 不变** — 设置了 `RESTART_CMD` 时，重启执行的是重载命令（如 `nginx -s reload`），而不是 stop+start。master 进程从未退出，所以 PID 保持不变。worker 进程被优雅地替换，不会中断连接。

### Redis（macOS Homebrew 安装）

```toml
[services.redis]
EXEC_CMD = '/opt/homebrew/opt/redis/bin/redis-server /opt/homebrew/etc/redis.conf'
RESTART_POLICY = 'on-failure'
BACK_OFF = 5
WORK_DIR = '/opt/homebrew/var'
STDOUT = '/opt/homebrew/var/log/redis.log'
STDERR = '/opt/homebrew/var/log/redis.log'
```

- `EXEC_CMD` 指向 Homebrew 安装的 Redis 二进制和配置文件
- `WORK_DIR` 是 Redis 数据目录（Apple Silicon 为 `/opt/homebrew/var`，Intel 为 `/usr/local/var`）
- `STDOUT` / `STDERR` 指向同一个文件没问题，两者都以追加模式打开

### Go 应用

```toml
[services.myapp]
EXEC_CMD = './myapp'
WORK_DIR = '/opt/sites/myapp'
RESTART_POLICY = 'on-failure'
BACK_OFF = 5
STDOUT = '/tmp/gorch/logs/myapp.out.log'
STDERR = '/tmp/gorch/logs/myapp.err.log'
```

### Godoc

```toml
[services.godoc]
EXEC_CMD = './bin/godoc -http=127.0.0.1:6060'
WORK_DIR = '/Users/ryan/gopath'
RESTART_POLICY = 'on-failure'
BACK_OFF = 5
STDOUT = '/tmp/gorch/logs/godoc.out.log'
STDERR = '/tmp/gorch/logs/godoc.err.log'
```

### Cron 定时任务

设置了 `CRON` 的服务按计划运行，不是常驻进程。gorch 会跳过重叠执行。

```toml
[services.daily_backup]
EXEC_CMD = './backup.sh'
WORK_DIR = '/opt/backup'
CRON = '0 0 2 * * *'        # 每天凌晨 2 点
CRON_TIMEOUT = 3600         # 超过 1 小时就杀掉
STDOUT = '/tmp/gorch/logs/backup.out.log'
STDERR = '/tmp/gorch/logs/backup.err.log'
```

## 重启行为

### 普通重启（stop + start）

对于大多数服务，重启（Web 界面或 `POST /api/services/<名称>/restart`）会先停止进程，再重新启动。PID 会变化，因为创建了新的进程。

### 优雅重载（RESTART_CMD）

设置了 `RESTART_CMD` 时，重启会执行该命令而非 stop+start。进程保持运行 — 只有 worker 或配置被重新加载。PID 不变。

这对于支持 reload 信号的守护进程（如 Nginx/Angie）非常理想：

1. Master 进程保持运行（PID 不变）
2. Worker 进程被优雅停止并替换为新进程
3. 不中断连接，无停机时间

如果即使设置了 `RESTART_CMD` 也想强制完全 stop+start，先停止再启动：

```sh
curl -u admin:secret -X POST http://127.0.0.1:8080/api/services/nginx/stop
curl -u admin:secret -X POST http://127.0.0.1:8080/api/services/nginx/start
```
