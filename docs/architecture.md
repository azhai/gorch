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

`/api/events` pushes a `status_change` for every service whose state differs
from the previous round, then one `uptime_tick` with the live pid, memory and
start time of whatever is running; both carry a `payload` object, which is what
the SPA's `applyMessage` reads. Every round allocates from a scratch arena that
is emptied first, so a stream left open for weeks does not grow the process.

`POST /api/services/<name>/start|stop|restart` answers with the service as it
stands after the call, so the dashboard repaints from the reply itself. Stop and
Restart mark the service `stopping` immediately — SIGTERM is in flight and the
exit is only observed on the next poll — and the SPA keeps polling that one
service for up to two seconds until the state is final; an SSE frame can be a
minute away on the slow intervals.

The frontend is plain ES modules plus a vendored Alpine build — no bundler.
`tools/embedgen` packs `web/` into a single raw-deflate blob (`assets.bin`)
plus an index table; the server inflates it once at startup. A `web/` directory
next to the config overrides the embedded copy file by file, so frontend edits
take effect without a rebuild.

### Memory and threads (`src/lib/worker`, `web`)

Every long-lived thread — the supervisor loop, the cron loop, cron fires and
the web listener — is a detached pthread we create ourselves with a fixed
512 KiB stack. `core:thread` sizes the stack from RLIMIT_STACK, which is 8 MiB
on glibc, musl and Darwin alike; a couple of dashboard tabs then reserve tens of
megabytes of address space, which is a real constraint on a 256 MiB router. Each
connection also gets its own thread and a 64 KiB request arena, so everything a
request allocates (including `fmt.tprintf` temporaries) dies with the connection
instead of going through the shared heap.

The logger formats every record into one process-wide buffer instead of taking
256 bytes per line from `context.temp_allocator` — a thread-local, append-only
arena that would otherwise push RSS up a line at a time for as long as the
process runs. The build also lowers `DEFAULT_TEMP_ALLOCATOR_BACKING_SIZE` from
its 4 MiB default to 64 KiB, so any path still using the temp arena starts at a
few pages.

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
| `src/lib/worker` | Long-lived threads (detached pthreads with a 512 KiB stack) |
| `src/lib/jwt`, `totp`, `qr`, `paths` | Auth, 2FA, default paths |
| `web/` | Alpine SPA (Material 3 Monet); served as-is, no bundler |
| `tools/embedgen` | Packs `web/` into `src/lib/web/assets.bin` + `assets_gen.odin` |
| `scripts/` | `gorch-install` / `gorch-uninstall` for systemd and launchd |
| `vendor/oboe` | Vendored submodule — `oboe:cache` state store |
| `third_party/zlib` | Minimal zlib sources, linked into static Linux builds |

## Build

`make one` builds the local binary, `make run` builds it and starts it, and
`make all` builds the local binary plus the four-target matrix (darwin/linux ×
arm64/amd64) and writes `bin/SHA256SUMS`. macOS links natively (macOS 11+);
Linux compiles to an object file and links with `zig cc` against static musl
plus the vendored zlib. Release builds use `-o:size`, strip symbols, and set the
default temp-arena block to 64 KiB. `VERSION` comes from `git describe`. There
is no UPX target: the packed image is unpacked into anonymous writable memory,
so its pages can be neither shared with the page cache nor reclaimed under
memory pressure.

## Tech Stack

- **Odin** (`dev-2026-10` or newer) — the whole implementation
- **Odin core** — `core:net`, `os`, `sync`, `thread`, `time`, `compress/zlib`, `crypto*`, posix/darwin syscalls
- **oboe:cache** — compressed, atomically snapshotted state store (vendored submodule)
- **zlib** — vendored minimal sources for static Linux linking
- **Alpine.js 3 + Material 3 (Monet) + ES modules** — Web UI, no build step
- **UPX** — not used: the packed image cannot share page-cache pages, which hurts on memory-constrained targets
- **systemd / launchd** — service supervision via `scripts/gorch-*`
