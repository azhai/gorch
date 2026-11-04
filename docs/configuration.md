# Configuration

## File Format

TOML. Without `-c` the config is looked up in this order:

1. `$GORCH_CONFIG`
2. `./gorch.toml`
3. `~/.config/gorch/gorch.toml`
4. `/etc/gorch/gorch.toml`

Use `-c /path/to/config.toml` to point at a specific file.

## Global Fields

| Field | Default | Description |
|-------|---------|-------------|
| `LOG_DIR` | `/tmp/gorch/logs` | Default log directory; services without explicit `STDOUT`/`STDERR` use `<LOG_DIR>/<name>.out.log` and `<LOG_DIR>/<name>.err.log` |
| `PID_FILE` | `/tmp/gorch/gorch.pid` | Compatibility field — parsed and saved back, but no PID file is written |
| `SERVICES_LOCK` | `/tmp/gorch/gorch-services.lock` | Compatibility field — parsed and saved back, but no lock file is written |

### Compatibility Fields (Not Applied)

The Odin rewrite kept these keys so existing `gorch.toml` files and the Web
config editor still load and round-trip them, but no runtime code consumes them
yet:

| Field | What the Go version did | In the Odin build |
|-------|-------------------------|-------------------|
| `PID_FILE` (global) | supervisor PID file | nothing is written |
| `SERVICES_LOCK` | pid list for adopting processes across restarts | nothing is written; state lives in the oboe cache snapshot `gorch.state` |
| `PID_FILE` (service) | daemon PID file used to find the real master | not read when spawning |
| `CHECK_PORT` | kill whatever holds a TCP port before start | validated (0–65535) but never checked |
| `ENV_VARS` | extra environment for the child | not passed; the child inherits gorch's own environment |

`DEPENDS_ON` is partially wired: unknown names, self-dependencies and cycles
are rejected at load time, but startup walks services in **alphabetical name
order**, not dependency order. If ordering matters, encode it in `PRE_ACTION`
or in the command itself.

### Log Rotation

Every service log (`STDOUT` / `STDERR`) is rotated once a day: when the calendar
day changes, the current file is copied to `<path>.<YYYY-MM-DD>` and the original
is truncated back to zero, so `STDOUT` always points at the current day's log.

Copy-then-truncate (rather than rename) is what makes this safe for a running
service. gorch hands the log file directly to the service, which keeps that file
descriptor for its whole lifetime — and keeps it even if gorch itself is
restarted — so the file has to stay at the same path; renaming it would leave
the service writing into the rotated file forever. Because gorch does the
rotating, a file that was written while gorch was down is split at the next
startup using the day it was last written to.

Non-regular targets such as `/dev/null` or a named pipe are never rotated.

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
| `TOTP_SECRET` | — | 32-byte master key as 64 hex characters (`openssl rand -hex 32`); without it the second factor stays off |
| `TOTP_DB` | `/var/lib/gorch/totp.db` | Path to the JSON store holding TOTP bindings, backup codes and used windows — **not** SQLite |

## Service Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `EXEC_CMD` | Yes, unless `CRON` is set | — | Command run through `/bin/sh -c`; plain commands get an `exec` prefix; max 1024 bytes |
| `RESTART_CMD` | No | — | Shell command for graceful reload instead of stop+start (e.g. `nginx -s reload`); 15 s timeout |
| `WORK_DIR` | No | Config file directory | Working directory for the process |
| `RESTART_POLICY` | No | `never` | `always` / `on-failure` / `never` |
| `BACK_OFF` | No | `0` (falls back to `5`) | Seconds to wait before a restart attempt |
| `PRE_ACTION` | No | — | Shell command run via `/bin/sh -c` before start; 15 s timeout, failure only warns |
| `CHECK_PORT` | No | `0` | Validated but not applied — see Compatibility Fields |
| `PID_FILE` | No | — | Compatibility field, not applied |
| `STDOUT` | No | `LOG_DIR/<name>.out.log` | Stdout log file path |
| `STDERR` | No | `LOG_DIR/<name>.err.log` | Stderr log file path |
| `DEPENDS_ON` | No | `[]` | Validated (unknown names / self / cycles), but does not order startup |
| `CRON` | No | — | 5- or 6-field cron expression; see Cron Expressions |
| `CRON_TIMEOUT` | No | `0` (off) | Seconds; a cron run still alive after this long is killed with SIGKILL |
| `ENV_VARS` | No | `{}` | Compatibility field, not applied |

### Startup Sequence

When a service starts (or restarts), gorch runs the following steps in order:

1. **PRE_ACTION** — if set, run via `/bin/sh -c` in `WORK_DIR` with a 15 s timeout. A failure is logged but does not block startup.
2. **Log files** — `STDOUT` / `STDERR` are opened in append mode and handed to the child as its file descriptors.
3. **EXEC_CMD** — launched with `/bin/sh -c`. A plain command (no shell metacharacters) is prefixed with `exec`, so the pid gorch tracks is the service itself; commands are capped at 1024 bytes.

### Graceful Reload via RESTART_CMD

For daemons that support a reload signal (e.g. `nginx -s reload`, `angie -s reload`), set `RESTART_CMD` so that restarting the service (from the Web UI or `POST /api/services/<name>/restart`) runs that command instead of killing and restarting the process. It has a 15 s timeout; if it fails, the API returns an error and the service is left running.

### Process Adoption on Restart

gorch tracks the pid it spawned itself. On restart it reads the recorded pid from
`gorch.state` (an oboe cache snapshot) and only adopts a still-running child
when both the pid is alive **and** its process start time matches — a reused pid
is rejected rather than killed by mistake. The Odin build does **not** scan for
a forked master by executable name or PPID=1; that belonged to the Go version.

## Environment Variable Expansion

`${VAR}` is expanded at load time, but only in these service fields:
`EXEC_CMD`, `WORK_DIR`, `RESTART_CMD`, `PRE_ACTION`, `STDOUT`, `STDERR` and
`PID_FILE`. An unset variable is a **config error** (gorch refuses to start),
not an empty string. Other fields, including values inside `ENV_VARS`, are not
expanded.

```toml
[services.app]
EXEC_CMD = "/app/bin/start --port ${PORT}"
WORK_DIR = "${HOME}/projects/app"
```

## Runtime Mode

`GORCH_MODE` sets the minimum log level:

| Value | Log Level | Use Case |
|-------|-----------|----------|
| `dev` | `debug` | Troubleshooting — reserved for debug output |
| anything else (default) | `info` | Production |

The current build emits no debug-level records, so `dev` and production produce
the same output for now.

## Cron Expressions

5 or 6 whitespace-separated fields — with 6 the first is seconds, with 5
seconds default to `0`:

```
6 fields:  second minute hour day-of-month month day-of-week
5 fields:          minute hour day-of-month month day-of-week

second 0-59 | minute 0-59 | hour 0-23 | dom 1-31 | month 1-12 | dow 0-7 (0 and 7 = Sunday)
```

Examples: `0 */30 * * * *` (every 30 min), `0 0 8 * * 1-5` (8am weekdays),
`0 2 * * *` (02:00 every day, 5-field form).

Shortcuts are also accepted: `@every 30s` / `@every 5m` / `@every 2h`, plus
`@yearly` (`@annually`), `@monthly`, `@weekly`, `@daily` (`@midnight`),
`@hourly` and `@minutely`.

Cron services are not started at boot — the scheduler runs them. A manual
`start` triggers one run immediately without disturbing the schedule, and a
`restart` ends the run currently in flight. Overlapping runs are detected and
skipped. Use `CRON_TIMEOUT` (seconds) to kill a run that hangs.

## Full Example

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

## Service Recipes

Common service configurations you can adapt.

### Nginx / Angie

Nginx and Angie use a multi-process architecture: a master process manages worker processes. Use `RESTART_CMD` for graceful reload — the master PID stays the same, only workers are restarted.

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

**Why PID doesn't change on restart** — When `RESTART_CMD` is set, a restart runs the reload command (e.g. `nginx -s reload`) instead of stop+start. The master process never exits, so its PID stays the same. Workers are replaced gracefully without dropping connections.

### Redis (Homebrew on macOS)

```toml
[services.redis]
EXEC_CMD = '/opt/homebrew/opt/redis/bin/redis-server /opt/homebrew/etc/redis.conf'
RESTART_POLICY = 'on-failure'
BACK_OFF = 5
WORK_DIR = '/opt/homebrew/var'
STDOUT = '/opt/homebrew/var/log/redis.log'
STDERR = '/opt/homebrew/var/log/redis.log'
```

- `EXEC_CMD` points to the Homebrew-installed Redis binary and config
- `WORK_DIR` is the Redis data directory (`/opt/homebrew/var` on Apple Silicon, `/usr/local/var` on Intel)
- Both log fields pointing at the same file is fine; they are opened in append mode

### Go Application

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

### Cron Job (scheduled task)

Services with `CRON` run on schedule, not continuously. gorch skips overlapping runs.

```toml
[services.daily_backup]
EXEC_CMD = './backup.sh'
WORK_DIR = '/opt/backup'
CRON = '0 0 2 * * *'        # 2:00 AM every day
CRON_TIMEOUT = 3600         # kill a run that exceeds an hour
STDOUT = '/tmp/gorch/logs/backup.out.log'
STDERR = '/tmp/gorch/logs/backup.err.log'
```

## Restart Behavior

### Normal restart (stop + start)

For most services, restarting (Web UI or `POST /api/services/<name>/restart`) stops the process and starts it again. The PID changes because a new process is created.

### Graceful reload (RESTART_CMD)

When `RESTART_CMD` is set, a restart runs that command instead of stop+start. The process stays alive — only its workers or configuration are reloaded. The PID doesn't change.

This is ideal for daemons like Nginx/Angie that support reload signals:

1. Master process stays running (PID unchanged)
2. Workers are gracefully stopped and replaced with new ones
3. No dropped connections, no downtime

If you want to force a full stop+start even when `RESTART_CMD` is set, stop the service first, then start it:

```sh
curl -u admin:secret -X POST http://127.0.0.1:8080/api/services/nginx/stop
curl -u admin:secret -X POST http://127.0.0.1:8080/api/services/nginx/start
```
