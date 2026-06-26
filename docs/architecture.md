# Architecture

```
CLI ──► Unix Socket IPC ──► Supervisor ──► Echo Web Server
                                   │
                                   ├── Process Manager
                                   ├── Cron Scheduler
                                   ├── Log Manager
                                   └── Status Cache
```

## Components

### Supervisor
Core orchestrator. Manages process lifecycle, cron scheduling, and status tracking.

### Process Manager
Starts, stops, and monitors individual processes. Handles restart policies, back-off, and daemonized process tracking.

### Cron Scheduler
6-field cron expression parser and scheduler. Triggers service execution on schedule. Detects and skips overlapping runs.

### Log Manager
Log file management — reading, clearing, purging. Part of the `gobus/log` shared library.

### Status Cache
Caches service status snapshots for fast queries and SSE updates.

### IPC Layer
Unix socket-based inter-process communication. CLI commands talk to the running supervisor through `/var/run/gorch.sock`.

### Web Server
Echo-based HTTP server with embedded React SPA. Provides REST API and real-time SSE updates.

## Tech Stack

- **Go** — Core runtime
- **Cobra** — CLI framework
- **Echo** — HTTP server
- **go-toml/v2** — TOML parsing
- **React + TypeScript + Tailwind** — Web UI (embedded via embed)
- **gobus/log** — Log management
- **go-totp** — TOTP two-factor authentication
