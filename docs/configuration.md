# Configuration

## File Format

TOML format. Default path: `gorch.toml` in the current directory.

Use `-c /path/to/config.toml` to specify a different path.

## Global Fields

| Field | Default | Description |
|-------|---------|-------------|
| `LOG_DIR` | — | Default log directory; services without explicit `STDOUT`/`STDERR` will use `<LOG_DIR>/<name>.out.log` and `<LOG_DIR>/<name>.err.log` |

## Web UI Fields

| Field | Default | Description |
|-------|---------|-------------|
| `WEB_ENABLE` | `false` | Enable the web management interface |
| `WEB_ADDR` | `127.0.0.1:8080` | Listen address |
| `WEB_AUTH` | `false` | Enable login authentication |
| `WEB_USER` | — | Login username |
| `WEB_PASS` | — | Login password |
| `URL_PREFIX` | `""` | Sub-path mount (e.g. `"/gorch"`). No rebuild needed — injected at runtime. |
| `TOTP_ENABLE` | `false` | Enable TOTP two-factor authentication |
| `TOTP_SECRET` | — | 32-byte hex-encoded master key (64 chars). Generate with `openssl rand -hex 32` |
| `TOTP_DB` | `auth_totp.db` | SQLite database path for TOTP data |

## Service Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `EXEC_CMD` | Yes | — | Command to execute |
| `RESTART_CMD` | No | — | Shell command for graceful reload instead of stop+start (e.g. `nginx -s reload`) |
| `WORK_DIR` | No | Config file directory | Working directory for the process |
| `RESTART_POLICY` | No | `never` | `always` / `on-failure` / `never` |
| `BACK_OFF` | No | `0` (auto-restart uses `2`) | Seconds to wait before restart attempt |
| `PRE_ACTION` | No | — | Shell command run via `sh -c` before start |
| `CHECK_PORT` | No | `0` | If set, kill any process occupying this port before start |
| `PID_FILE` | No | — | PID file path for daemonized processes |
| `STDOUT` | No | `LOG_DIR/<name>.out.log` | Stdout log file path |
| `STDERR` | No | `LOG_DIR/<name>.err.log` | Stderr log file path |
| `DEPENDS_ON` | No | `[]` | Services that must start first (topological order) |
| `CRON` | No | — | 6-field cron expression (with seconds) for scheduled runs |
| `ENV_VARS` | No | `{}` | Environment variables passed to the process |

### Startup Sequence

When a service starts (or restarts), gorch runs the following steps in order:

1. **PRE_ACTION** — if set, executed via `sh -c` in `WORK_DIR`. Failures are logged but do not block startup.
2. **CHECK_PORT** — if `> 0`, kills any process listening on that port (`lsof` + `SIGKILL`).
3. **StartProcess** — launches `EXEC_CMD`.

### Graceful Reload via RESTART_CMD

For daemons that support a reload signal (e.g. `nginx -s reload`, `angie -s reload`), set `RESTART_CMD` so that `gorch restart <name>` triggers a graceful reload instead of killing and restarting the process.

### Daemonized Process Tracking

Daemons like Nginx/Angie fork a master process and exit the original PID. The master is then reparented to init (PPID=1). gorch locates the real master by matching the executable name and preferring the process with PPID=1, falling back to the smallest matching PID.

## Environment Variable Expansion

Use `${VAR}` syntax in string fields — they will be expanded from the environment at load time:

```toml
[services.app]
EXEC_CMD = "/app/bin/start --port ${PORT}"
WORK_DIR = "${HOME}/projects/app"
```

## Runtime Mode

Set `GORCH_MODE` to control log verbosity:

| Value | Log Level | Use Case |
|-------|-----------|----------|
| `dev` | `debug` | Troubleshooting — shows process state/etime/rss/cmd per tick |
| `prod` (default) | `info` | Production |

## Cron Expressions

6-field format with seconds:

```
┌──────── second (0-59)
│ ┌────── minute (0-59)
│ │ ┌──── hour (0-23)
│ │ │ ┌── day of month (1-31)
│ │ │ │ ┌─ month (1-12)
│ │ │ │ │ ┌ day of week (0-6, Sun=0)
│ │ │ │ │ │
* * * * * *
```

Examples: `0 */30 * * * *` (every 30 min), `0 0 8 * * 1-5` (8am weekdays)

Cron services cannot be started manually — they run on schedule. Overlapping runs are detected and skipped.

## Full Example

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

## Service Recipes

Common service configurations you can adapt.

### Nginx / Angie

Nginx and Angie use a multi-process architecture: a master process manages worker processes. Use `RESTART_CMD` for graceful reload — the master PID stays the same, only workers are restarted.

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

**Why PID doesn't change on restart** — When `RESTART_CMD` is set, `gorch restart` runs the reload command (e.g. `nginx -s reload`) instead of stop+start. The master process never exits, so its PID stays the same. Workers are replaced gracefully without dropping connections.

### Redis (Homebrew on macOS)

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

- `EXEC_CMD` points to the Homebrew-installed Redis binary and config
- `WORK_DIR` is the Redis data directory (`/opt/homebrew/var` on Apple Silicon, `/usr/local/var` on Intel)
- `CHECK_PORT = 6379` ensures no stale process holds the port on restart

### Go Application

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

### Static File Server (weblite)

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

### Cron Job (scheduled task)

Services with `CRON` run on schedule, not continuously. gorch skips overlapping runs.

```toml
[services.daily_backup]
EXEC_CMD = './backup.sh'
WORK_DIR = '/opt/backup'
CRON = '0 0 2 * * *'  # 2:00 AM every day
STDOUT = '/var/log/gorch/backup.out.log'
STDERR = '/var/log/gorch/backup.err.log'
```

## Restart Behavior

### Normal restart (stop + start)

For most services, `gorch restart <name>` stops the process and starts it again. The PID changes because a new process is created.

### Graceful reload (RESTART_CMD)

When `RESTART_CMD` is set, `gorch restart` runs that command instead of stop+start. The process stays alive — only its workers or configuration are reloaded. The PID doesn't change.

This is ideal for daemons like Nginx/Angie that support reload signals:

1. Master process stays running (PID unchanged)
2. Workers are gracefully stopped and replaced with new ones
3. No dropped connections, no downtime

If you want to force a full stop+start even when `RESTART_CMD` is set, stop the service first, then start it:

```sh
gorch stop -s nginx
gorch start -s nginx
```
