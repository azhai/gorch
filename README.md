# gorch

A lightweight process supervisor written in Go, inspired by [Supervisor](http://supervisord.org/).

Single binary, no runtime dependencies. Declarative TOML config. Built-in Web UI.

[简体中文](./README-ZH.md)


![Gorch Web UI](./screenshot-gorch.png)

## Install

### From Source

```sh
export PATH="$GOPATH/bin:$PATH"
go install github.com/azhai/gorch@latest
```

### Install as System Service

In macOS, suggest [launchd-ui](https://github.com/azu/launchd-ui) to manage the service.

```sh
gorch install            # system-wide (Linux: systemd, macOS: launchd)
gorch install --user     # user-level service
gorch uninstall          # remove
```

`gorch install` automatically writes the service file, loads and starts it. If the auto-start fails, load manually:

```sh
# macOS:
launchctl load -w ~/Library/LaunchAgents/com.github.azhai.gorch.plist
# Linux:
systemctl daemon-reload
systemctl enable --now gorch
```

## Quick Start

```sh
# 1. Create config
cp gorch.toml.example gorch.toml
# Edit gorch.toml to define your services

# 2. Start
gorch start                    # foreground, default config
gorch start -c /etc/gorch.toml # specify config path
gorch start -d                 # daemonize (background)

# 3. Check status
gorch status
gorch status -s api            # single service
gorch status --json            # JSON output

# 4. Control services
gorch restart -s api
gorch stop

# 5. View logs
gorch logs -s api              # last 100 lines
gorch logs -s api -n 500      # last 500 lines
```

## Configuration

Config file is TOML format. Default: `gorch.toml` in current directory.

### Minimal Example

```toml
[services.myapp]
EXEC_CMD = "python app.py"
```

### Full Example

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

### Service Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `EXEC_CMD` | Yes | — | Command to execute |
| `WORK_DIR` | No | Config file directory | Working directory for the process |
| `RESTART_POLICY` | No | `never` | `always` / `on-failure` / `never` |
| `BACK_OFF` | No | `0` | Seconds to wait before restart attempt |
| `STDOUT` | No | `LOG_DIR/<name>.out.log` | Stdout log file path |
| `STDERR` | No | `LOG_DIR/<name>.err.log` | Stderr log file path |
| `DEPENDS_ON` | No | `[]` | Services that must start first (topological order) |
| `CRON` | No | — | 6-field cron expression (with seconds) for scheduled runs |
| `ENV_VARS` | No | `{}` | Environment variables passed to the process |

### Web UI Fields

| Field | Default | Description |
|-------|---------|-------------|
| `WEB_ENABLE` | `false` | Enable the web management interface |
| `WEB_ADDR` | `127.0.0.1:8080` | Listen address |
| `WEB_AUTH` | `false` | Enable login authentication |
| `WEB_USER` | — | Login username |
| `WEB_PASS` | — | Login password |

### Global Fields

| Field | Default | Description |
|-------|---------|-------------|
| `LOG_DIR` | — | Default log directory; services without explicit STDOUT/STDERR will use `<LOG_DIR>/<name>.out.log` and `<LOG_DIR>/<name>.err.log` |

### Environment Variable Expansion

Use `${VAR}` syntax in string fields — they will be expanded from the environment at load time:

```toml
[services.app]
EXEC_CMD = "/app/bin/start --port ${PORT}"
WORK_DIR = "${HOME}/projects/app"
```

### Cron Expressions

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

## Web UI

Visit `http://<WEB_ADDR>` when `WEB_ENABLE = true`.

Features:
- **Dashboard** — Real-time service status with WebSocket updates, start/stop/restart controls
- **Logs** — View stdout and stderr logs with tab switching
- **Config** — Edit service configuration with two-step save: Apply (memory) then Save to File (persist)
- **Cron Validation** — Validate cron expressions and preview next run times

## CLI Reference

| Command | Description |
|---------|-------------|
| `gorch start [-c config] [-d]` | Start services (`-d` to daemonize) |
| `gorch stop` | Stop all services |
| `gorch restart -s <name>` | Restart a service |
| `gorch status [-s name] [-j]` | Show status (`-j` for JSON) |
| `gorch logs -s <name> [-n lines]` | View service logs |
| `gorch install [--user]` | Install as system service |
| `gorch uninstall [--user]` | Uninstall system service |

## Architecture

```
CLI ──► Unix Socket IPC ──► Supervisor ──► Fiber Web Server
                                   │
                                   ├── Process Manager
                                   ├── Cron Scheduler
                                   ├── Log Manager
                                   └── Status Cache
```

## Tech Stack

- **Go** — Core runtime
- **Cobra** — CLI framework
- **Fiber** — HTTP server
- **robfig/cron** — Cron scheduling
- **go-toml/v2** — TOML parsing
- **React + TypeScript + Tailwind** — Web UI (embedded via embed)

## License

MIT
