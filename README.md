# gorch

A lightweight process supervisor written in Go, inspired by [Supervisor](http://supervisord.org/).

## Features

- **Declarative Configuration** — TOML-based service definitions
- **Dependency Management** — Topological startup/shutdown ordering
- **Restart Policies** — `always`, `on-failure`, `never` with back-off
- **Cron Scheduling** — Scheduled tasks with overlap detection
- **Web Management UI** — Real-time dashboard with WebSocket updates
- **Daemon Mode** — Background execution with Unix Socket IPC
- **Log Management** — File rotation and real-time log streaming
- **Single Binary** — No runtime dependencies, frontend embedded

## Quick Start

### Build

```sh
make one          # local build (current platform)
make build        # cross-compile (darwin/linux/windows × arm64/amd64)
make all          # clean → one → build (full pipeline)
```

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make one` | Local single-platform build — compiles `gorch` binary + all `cmd/*` commands |
| `make build` | Cross-compile for 5 platforms: darwin-arm64/amd64, linux-arm64/amd64, windows-amd64 |
| `make all` | Full pipeline: clean → local build → cross-compile |
| `make front` | Build frontend only (`webui/` via npm/Vite) |
| `make run` | Build frontend + `go run ./` |
| `make dev` | Hot-reload dev server (air for backend, vite HMR for frontend) |
| `make clean` | Remove `bin/`, `tmp/`, and frontend dist |
| `make test` | Run Go tests with verbose output |
| `make lint` | Run golangci-lint |
| `make tidy` | Run go mod tidy |

### Project Structure

```
gorch/
├── main.go              # entry point (SINGLETON binary: gorch)
├── cmd/
│   └── weblite/         # additional command binaries (auto-discovered)
│       ├── cmd.go
│       └── server.go
├── internal/            # core packages
│   ├── config/          # TOML config loader
│   ├── supervisor/      # process supervisor
│   ├── web/             # Fiber HTTP server + WebSocket
│   ├── cron/            # cron scheduler
│   ├── ipc/             # Unix socket IPC
│   └── status/          # status cache & snapshot
├── webui/               # React + TypeScript + Tailwind frontend (embedded)
│   └── src/
├── gorch.toml           # configuration file
└── Makefile             # build system
```

### How COMMANDS Auto-Discovery Works

The Makefile automatically discovers subdirectories under `cmd/`:
- Each directory in `cmd/` becomes a separate build target
- The **main** binary (`gorch`) is built from `./` (root `main.go`)
- **Command** binaries are built from `./cmd/<name>/`
- Adding a new command is as simple as creating a new directory under `cmd/`

### Version Injection

Builds automatically inject the git version string into the binary:

```sh
VERSION=$(git describe --tags) make one
# or let it auto-detect (defaults to "dev" if no tags)
```

### Testing

```sh
make test              # run all Go unit tests (52 tests across 5 packages)
./test_makefile.sh     # dry-run tests for Makefile targets
```

#### Test Coverage

| Package | Tests | Coverage |
|---------|-------|----------|
| `internal/config` | 20 | TOML parsing, validation, env expansion, topological sort, circular deps, cron config |
| `internal/status` | 9 | Cache CRUD, state save/load JSON roundtrip |
| `internal/cron` | 14 | Scheduler lifecycle, job registration, execution records (capped at 10), overlap detection |
| `internal/ipc` | 6 | Protocol serialization, Ok/ErrorResponse, all action types |
| `internal/supervisor` | 15 | Constructor/options, status queries, config update, command handling, EnsureDir |

### Configuration

Create a `gorch.toml`:

```toml
[services.api]
EXEC_CMD = "python manage.py runserver 0.0.0.0:8000"
WORK_DIR = "/app/backend"
RESTART_POLICY = "on-failure"
BACK_OFF = 5
STDOUT = "/var/log/api.stdout.log"
STDERR = "/var/log/api.stderr.log"
DEPENDS_ON = ["postgres"]
CRON = "0 */30 * * * *"
ENV_VARS = { DEBUG = "true" }

[services.postgres]
EXEC_CMD = "postgres -D /var/lib/postgres"
RESTART_POLICY = "always"
BACK_OFF = 3

[web]
WEB_ENABLE = true
WEB_ADDR = "127.0.0.1:8080"
```

### Usage

| Command | Description |
|---------|-------------|
| `gorch start` | Start services in foreground |
| `gorch start -c app.toml` | Start with specific config |
| `gorch start --daemonize` | Run as background daemon |
| `gorch status` | Show service status |
| `gorch status --json` | JSON output |
| `gorch logs -s api` | View service logs |
| `gorch restart -s api` | Restart a service |
| `gorch stop` | Stop all services |

### Service Configuration Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `EXEC_CMD` | Yes | — | Command to execute |
| `WORK_DIR` | No | Config file directory | Working directory |
| `RESTART_POLICY` | No | `never` | `always`, `on-failure`, or `never` |
| `BACK_OFF` | No | `0` | Seconds to wait before restart |
| `STDOUT` | No | Supervisor log | Stdout log file path |
| `STDERR` | No | Same as STDOUT | Stderr log file path |
| `DEPENDS_ON` | No | `[]` | List of service dependencies |
| `CRON` | No | — | Cron expression for scheduled runs |
| `ENV_VARS` | No | `{}` | Environment variables map |

### Web UI

When `WEB_ENABLE = true`, visit `http://127.0.0.1:8080` for:
- Real-time service status dashboard
- Service start/stop/restart controls
- Log viewer with live streaming
- Read-only configuration view

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

- **Go 1.26** — Core runtime
- **Cobra** — CLI framework
- **Fiber** — HTTP server
- **robfig/cron** — Cron scheduling
- **pelletier/go-toml/v2** — TOML parsing
- **React + TypeScript + Tailwind** — Web UI (embedded)

## License

MIT
