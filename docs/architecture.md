# Architecture

`gorch` is a single Odin binary that runs in the foreground. It loads a TOML
config, starts the configured services, applies restart policies and cron
schedules, and serves the dashboard plus the entire control API over HTTP.
There is no CLI server, no Unix socket and no helper binary — everything lives
in one process, supervised by systemd or launchd through `scripts/gorch-install`.

```
gorch.toml ──► config / toml ──► Supervisor ──► managed services
                                     │  ├── restart policy + back-off
                                     │  ├── cron scheduler
                                     │  ├── RSS sampler
                                     │  └── stdout / stderr log rotation
                                     │
                                     ├── state (oboe cache → gorch.state)
                                     └── status snapshots
                                                  │
                        core:net HTTP server ────┴──► browser
                          ├── embedded SPA
                          ├── REST API + SSE stream
                          └── JWT login / TOTP 2FA
```

## Components

### Supervisor (`src/lib/supervisor`)

The main loop: starts and reaps processes, applies restart policies and
back-off, triggers cron services, rotates logs, and every few seconds samples
each service's resident memory summed over its whole process tree. Kernel reads
(`/proc` on Linux, libproc on macOS) happen outside the supervisor lock, so a
slow read never blocks the API.

### Platform layer (`proc_start_*.odin`, `proc_rss_*.odin`)

Linux walks `/proc`; macOS calls libproc. Process start times also detect pid
reuse, so a stale pid is never adopted after a restart.

### Cron (`src/lib/cron`)

6-field cron expression parser. Overlapping runs are skipped.

### State and status (`src/lib/state`, `src/lib/status`)

`state` keeps pids, exit codes and restart counts in an `oboe:cache` snapshot
written atomically to `gorch.state` — no database, no pid files. `status`
defines the snapshot records served to the dashboard.

### Config and parsers (`src/lib/config`, `toml`, `json`)

Hand-written TOML and JSON parsers. The JSON reader is cursor based: values
consumed in place borrow the request body, while anything persisted uses an
owning cursor that clones its strings (request bodies live on the connection
stack).

### Logs (`src/lib/logfile`, `log`)

Per-service stdout/stderr files with time-based rotation.

### Web layer (`src/lib/web`, `web/`)

A `core:net` HTTP server; each connection runs on its own thread so a
long-lived SSE stream never blocks the dashboard. It serves the SPA, injects
`window.__URL_PREFIX__` for sub-path deployments, exposes the REST API and
SSE, and handles JWT login and TOTP 2FA.

The frontend is plain ES modules plus a vendored Alpine build — no bundler.
`tools/embedgen` packs `web/` into a single raw-deflate blob (`assets.bin`)
plus an index table; the server inflates it once at startup. A `web/` directory
next to the config overrides the embedded copy file by file, so frontend edits
take effect without a rebuild.

### Auth (`src/lib/jwt`, `totp`, `qr`)

Optional HTTP authentication and TOTP two-factor enrolment, including QR code
generation.

## Source Layout

| Path | Contents |
|------|----------|
| `src/cmd/gorch` | `package main` — the only entry point (`-c/-h/-v`) |
| `src/lib/supervisor` | Process lifecycle, cron triggering, restart policy, RSS sampling |
| `src/lib/web` | HTTP transport, static assets, REST API, SSE, auth glue |
| `src/lib/config`, `toml`, `json` | Config structs and hand-written parsers |
| `src/lib/state`, `status` | oboe cache snapshots and status records |
| `src/lib/cron`, `logfile`, `log` | Cron expressions, log rotation, internal logging |
| `src/lib/jwt`, `totp`, `qr`, `paths` | Auth, 2FA, default paths |
| `web/` | Alpine SPA (Material 3 Monet); served as-is, no bundler |
| `tools/embedgen` | Packs `web/` into `src/lib/web/assets.bin` + `assets_gen.odin` |
| `scripts/` | `gorch-install` / `gorch-uninstall` for systemd and launchd |
| `vendor/oboe` | Vendored submodule — `oboe:cache` state store |
| `third_party/zlib` | Minimal zlib sources, linked into static Linux builds |

## Build

`make one` builds the local binary; `make release` builds the four-target
matrix (darwin/linux × arm64/amd64) and UPX-packs the two Linux binaries.
macOS links natively (macOS 11+); Linux compiles to an object file and links
with `zig cc` against static musl plus the vendored zlib. Release builds use
`-o:size` and strip symbols. `VERSION` comes from `git describe`.

## Tech Stack

- **Odin** (`dev-2026-10` or newer) — the whole implementation
- **Odin core** — `core:net`, `os`, `sync`, `thread`, `time`, `compress/zlib`, `crypto*`, posix/darwin syscalls
- **oboe:cache** — compressed, atomically snapshotted state store (vendored submodule)
- **zlib** — vendored minimal sources for static Linux linking
- **Alpine.js 3 + Material 3 (Monet) + ES modules** — Web UI, no build step
- **UPX** — optional `--brute` packing of Linux release binaries
- **systemd / launchd** — service supervision via `scripts/gorch-*`
