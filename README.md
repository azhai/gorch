# gorch

Lightweight process supervisor written in Odin, inspired by [Sysg](https://github.com/ra0x3/systemg) and [Supervisor](http://supervisord.org/).

Single binary, no runtime dependencies. Declarative TOML config. Built-in Web UI.

[简体中文](./README-ZH.md)

![Gorch Web UI](./screenshot-gorch.png)

## Why gorch?

- **Web UI + REST API** — Manage every service from a browser dashboard or script it against a small JSON API. The Odin build dropped the Go version's CLI subcommands; installing as a service is handled by two shell scripts.
- **TOTP 2FA for web login** — Account credentials are stored in the config file, but with TOTP two-factor authentication, even if the password leaks, your dashboard stays protected.
- **Single binary, zero dependencies** — One file, drop it anywhere and run.
- **Graceful reload support** — For Nginx/Angie-style daemons, `RESTART_CMD` keeps the master PID stable while reloading workers.

## When NOT to use gorch

gorch is great for web apps, API servers, cron jobs, and stateless daemons. It is **not recommended** for:

- **Traditional databases** (MySQL, PostgreSQL, etc.) — Frequent restarts can lead to data inconsistency or corruption. Use the database's own management tools instead.

## Binary

One binary is built from this repo: **`gorch`** — a process supervisor that manages long-running services and cron jobs, with a built-in Web UI and REST API.

It is written in Odin. The Web UI is embedded as compressed assets, so a single file is all you deploy; a `web/` directory next to the config still wins over the embedded copy file by file.

> **The Odin binary has no subcommands.** There is no `install`, `uninstall`,
> `start`, `stop`, `restart`, `status` or `logs` verb. Installing / removing the
> system service is done by two shell scripts; controlling services is done from
> the Web UI or REST API. Those subcommands belong to the Go implementation on
> the `lite` branch.

## Install

### 1. Build the binary

Requires [Odin](https://odin-lang.org/) `dev-2026-10` or newer on the **build** machine; the target machine needs nothing (Linux binaries are static musl, macOS binaries target macOS 11+).

```sh
git clone https://github.com/azhai/gorch.git
cd gorch
git checkout v0.4.5      # or the tag you want to deploy
make deps                # fetch vendor/oboe, the only submodule
make one                 # build ./bin/gorch for this machine
```

For release artifacts of macOS / Linux × arm64 / amd64:

```sh
make all                                     # local + four targets + bin/SHA256SUMS
make all TARGETS="linux_arm64"               # a single target only
make all CROSS_CC="x86_64-linux-musl-gcc"    # override the cross linker (default: zig cc)
make run                                     # dev loop: build locally, then start it
make test                                    # every package that has tests
```

The full matrix is meant to be built on macOS: darwin targets link natively, linux targets are cross-linked with `zig cc` + static musl plus the vendored minimal zlib in `third_party/zlib`; a native build uses the system zlib. On a Linux host, build only the `linux_*` targets. GitHub tags carry source archives only; binaries are produced by these `make` targets (or copied from a `bin/` you already built).

Nothing is UPX-packed: UPX unpacks the whole image into anonymous, writable memory, which can neither be shared with the page cache nor dropped under memory pressure. On a router, take the plain binary (about 630 KB for arm64; the filesystem cache keeps it warm anyway). Running `upx --brute bin/gorch-v0.5.1-linux-arm64` by hand still works if you want the smaller file.

### 2. Put the binary on the target

One file is enough — no runtime, no shared libraries, no `web/` directory. Verify the checksum (run it in `bin/`, or keep `SHA256SUMS` next to the artifact), then install it under the plain name `gorch` so the service scripts can find it:

```sh
sha256sum -c SHA256SUMS --ignore-missing     # Linux; on macOS: shasum -a 256 gorch-v0.4.5-darwin-arm64
chmod +x gorch-v0.4.5-linux-amd64
sudo install -m 755 gorch-v0.4.5-linux-amd64 /usr/local/bin/gorch
gorch --help
```

A `.upx` build you packed by hand also runs, but it is not recommended on memory-constrained devices. If the binary lives somewhere else, pass `--bin PATH` to `gorch-install` or set `GORCH_BIN`.

### 3. Install as a system service

The binary runs in the foreground, so a service manager is what keeps it alive. Two standalone POSIX `sh` scripts generate a systemd unit (Linux), a procd init script (OpenWrt / ImmortalWrt, e.g. arm64 routers) or a launchd plist (macOS):

```sh
sudo ./scripts/gorch-install --config /etc/gorch/gorch.toml    # system-wide (root required)
./scripts/gorch-install --user --config ~/.config/gorch/gorch.toml  # user-level, no root
sudo ./scripts/gorch-install --init procd --config /etc/gorch/gorch.toml  # force /etc/init.d/gorch
./scripts/gorch-install --dry-run --config /etc/gorch/gorch.toml   # preview only, writes nothing
```

On Linux `auto` (the default) picks procd when `/etc/openwrt_release` exists, or when both `/etc/rc.common` and `/sbin/procd` are present; otherwise it uses systemd. Pass `--init systemd|procd` to override. `--dry-run --init procd` also works on macOS to preview the init script before deploying.

| Option | `gorch-install` | `gorch-uninstall` |
|--------|:---------------:|:----------------:|
| `-c, --config PATH` | config file to use | — |
| `-b, --bin PATH` | binary to run | — |
| `-i, --init SYSTEM` | Linux init: `auto` / `systemd` / `procd` | same value as at install time |
| `-u, --user` | user-level service | user-level service |
| `-n, --no-start` | write the unit but do not enable/start | — |
| `-d, --dry-run` | preview, change nothing | preview, change nothing |
| `-h, --help` | show help | show help |

Config search order: `--config`, `$GORCH_CONFIG`, `/etc/gorch/gorch.toml`, `~/.config/gorch/gorch.toml`, `./gorch.toml` (`--user` puts the home path first). Binary search order: `--bin`, `$GORCH_BIN`, `./bin/gorch`, `./gorch`, `$PATH`.

| OS | Scope | Service file |
|----|-------|-------------|
| Linux | system | `/etc/systemd/system/gorch.service` |
| Linux (OpenWrt) | system | `/etc/init.d/gorch` (+ `/etc/rc.d/` links) |
| Linux | `--user` | `~/.config/systemd/user/gorch.service` |
| macOS | system | `/Library/LaunchDaemons/com.github.azhai.gorch.plist` |
| macOS | `--user` | `~/Library/LaunchAgents/com.github.azhai.gorch.plist` |

The generated definition is `gorch -c <config>` — `Type=simple` + `Restart=on-failure` on systemd, `KeepAlive` + `RunAtLoad` on launchd, and a minimal `USE_PROCD=1` script with `START=95` / `respawn 3600 5 0` on OpenWrt:

```sh
#!/bin/sh /etc/rc.common
START=95
STOP=10
USE_PROCD=1
start_service() {
  procd_open_instance gorch
  procd_set_param command "/usr/local/bin/gorch" -c "/etc/gorch/gorch.toml"
  procd_set_param respawn 3600 5 0
  procd_set_param stdout 1
  procd_set_param stderr 1
  procd_close_instance
}
```

There is no `start` verb and no `PIDFile`: the Odin binary does not fork. The script then runs `systemctl daemon-reload && systemctl enable --now gorch.service`, `/etc/init.d/gorch enable && /etc/init.d/gorch restart`, or `launchctl load -w <plist>` — all of which you can do by hand when `--no-start` was used. On a systemd user service, run `loginctl enable-linger <user>` if it must survive logout. See the [CLI Reference](./docs/cli.md) for every option and generated file. On macOS, [launchd-ui](https://github.com/azu/launchd-ui) is also handy for managing the resulting agent.

## Usage

### 1. Create the config

```sh
sudo mkdir -p /etc/gorch
sudo cp gorch.toml.example /etc/gorch/gorch.toml
```

Minimal config:

```toml
[services.myapp]
EXEC_CMD = "python app.py"
```

Real-world example:

```toml
LOG_DIR = '/tmp/gorch/logs'

[web]
WEB_ENABLE = true
WEB_AUTH = true
WEB_USER = "admin"
WEB_PASS = "secret"

[services.api]
EXEC_CMD = "python manage.py runserver 0.0.0.0:8000"
WORK_DIR = "/app/backend"
RESTART_POLICY = "on-failure"
BACK_OFF = 5
CHECK_PORT = 8000
STDOUT = "/tmp/gorch/logs/api.stdout.log"
STDERR = "/tmp/gorch/logs/api.stderr.log"
DEPENDS_ON = ["redis"]

[services.redis]
EXEC_CMD = "redis-server /etc/redis/redis.conf"
RESTART_POLICY = "always"
BACK_OFF = 3
```

### 2. Run gorch

```sh
gorch                             # foreground, config auto-detected
gorch -c /etc/gorch/gorch.toml    # explicit config
gorch --config=/etc/gorch/gorch.toml
gorch --help
gorch --version
```

| Flag | Default | Description |
|------|---------|-------------|
| `-c, --config <path>` | auto-detected | Config file path |
| `-h, --help` | | Print usage and exit |
| `-v, --version` | | Print version and exit |

Without `-c`, the first existing file wins: `$GORCH_CONFIG`, `./gorch.toml`, `~/.config/gorch/gorch.toml`, `/etc/gorch/gorch.toml`. If none exists, gorch exits with an error. `GORCH_WEB_DIR` overrides where the SPA assets are read from (the binary has an embedded copy).

> Note: `--version` currently prints a fixed string, not the release tag from
> `git describe`; use `SHA256SUMS` to identify an artifact.

There is **no daemonize flag** (`-d`): gorch stays in the foreground and stops every child before exiting on `SIGTERM`/`SIGINT`. For background operation, install it as a service (above).

### 3. Control gorch itself

Once installed as a service, use the service manager — not gorch subcommands:

```sh
# Linux / systemd (add --user for a user-level install)
sudo systemctl status gorch.service
sudo systemctl restart gorch.service
sudo systemctl stop gorch.service        # also stops all managed children
sudo journalctl -u gorch.service -f

# OpenWrt / procd
/etc/init.d/gorch status
/etc/init.d/gorch restart
/etc/init.d/gorch stop             # also stops all managed children
logread -e gorch                   # stdout / stderr go to the system log

# macOS / launchd
sudo launchctl list com.github.azhai.gorch
sudo launchctl unload -w /Library/LaunchDaemons/com.github.azhai.gorch.plist
sudo launchctl load -w /Library/LaunchDaemons/com.github.azhai.gorch.plist
```

On systemd, gorch's own output goes to the journal; on procd, to `logread`; on launchd, to `/tmp/gorch/logs/gorch.{out,err}.log`.

### 4. Control the managed services

Use the dashboard's Start / Stop / Restart buttons, or the same REST API:

```sh
BASE=http://127.0.0.1:8080
# WEB_AUTH = true: log in once, then reuse the JWT (valid 24h) for every call
TOKEN=$(curl -s -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"secret"}' | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["token"])')

curl -H "Authorization: Bearer $TOKEN" "$BASE/api/services"
curl -H "Authorization: Bearer $TOKEN" -X POST "$BASE/api/services/api/restart"
curl -H "Authorization: Bearer $TOKEN" -X POST "$BASE/api/services/api/stop"
```

With `WEB_AUTH = false`, drop the `-H` line entirely.

The API uses **Bearer JWT**, not HTTP Basic — `curl -u` returns 401 even with the right credentials. `/api/services` needs a token whenever `WEB_AUTH = true` (the unauthenticated request the UI makes on load to detect auth mode also gets 401 by design); only `/api/auth/*` is public. With `URL_PREFIX = "/gorch"` (e.g. behind a reverse proxy at `https://host/gorch/`), prepend the prefix to every UI and API path: `/gorch/api/services`.

### 5. View logs

The Web UI has a live log viewer (stdout / stderr); the API returns a JSON tail:

```sh
curl -H "Authorization: Bearer $TOKEN" 'http://127.0.0.1:8080/api/services/api/logs?type=stderr&lines=100'
```

### 6. Web UI

With `WEB_ENABLE = true`, open `http://127.0.0.1:8080` in your browser (default `WEB_ADDR`; set `URL_PREFIX` for a sub-path).

Features:
- Dashboard with real-time status (including per-service RSS)
- Log viewer (stdout / stderr)
- Config editor with two-step save
- Cron validation

## Uninstall

Uninstalling means removing the systemd unit / procd init script / launchd plist. The script stops and disables the service first (on procd it also drops the `/etc/rc.d/` symlinks), then deletes **only the service file**:

```sh
sudo ./scripts/gorch-uninstall                  # system-wide
./scripts/gorch-uninstall --user               # user-level
sudo ./scripts/gorch-uninstall --init procd    # OpenWrt, or when auto-detection differs
./scripts/gorch-uninstall --dry-run            # preview the actions, change nothing
```

The binary, config, logs and persisted state are intentionally kept, so reinstalling or upgrading is just replacing the binary and running `gorch-install` again. If you want a complete cleanup, stop the service first and remove these files yourself:

| Path | What it is |
|------|-----------|
| `/usr/local/bin/gorch` (or your `--bin` path) | The binary |
| `/etc/gorch/gorch.toml`, `~/.config/gorch/gorch.toml` | Config (contains web credentials) |
| `<LOG_DIR>`, default `/tmp/gorch/logs` | Service stdout / stderr logs |
| `/tmp/gorch/gorch.state`, `<LOG_DIR>/gorch.state` | Persisted service state |
| `TOTP_DB`, default `/var/lib/gorch/totp.db` | TOTP binding database |

Also run `loginctl disable-linger <user>` if you enabled lingering for a systemd user service. See the [CLI Reference](./docs/cli.md) for manual `systemctl` / `/etc/init.d/gorch` / `launchctl` equivalents.

## Runtime Files

On startup gorch creates `/tmp/gorch` and its `logs/` subdirectory. Everything it
writes lives there unless you point `LOG_DIR` elsewhere:

| Path | Purpose |
|------|---------|
| `<LOG_DIR>/<name>.out.log` | Service stdout (default when `STDOUT` is unset) |
| `<LOG_DIR>/<name>.err.log` | Service stderr (default when `STDERR` is unset) |
| `<LOG_DIR>/gorch.state` | Atomic snapshot of service state (oboe cache) |

`LOG_DIR` defaults to `/tmp/gorch/logs`; with no `LOG_DIR` set, the state file is
`/tmp/gorch/gorch.state`. `STDOUT` / `STDERR` override individual log paths.
`TOTP_DB` defaults to `/var/lib/gorch/totp.db` when TOTP is enabled.

The Odin build has no Unix socket, supervisor PID file or CLI ↔ supervisor IPC —
service control goes through the Web UI and REST API. The global `PID_FILE` and
`SERVICES_LOCK` keys are still accepted for config compatibility but are unused.
A **service-level** `PID_FILE` *is* used: when a command daemonizes (nginx/angie),
gorch reads it to find the real master; without one, a same-named process
reparented to init is adopted. A service with no known pid reports
`"uptime": null` and the dashboard shows a dash.

> **Note:** `/tmp` is cleared on reboot (and periodically on macOS), so logs do not survive a restart. Point `LOG_DIR` at a persistent directory if you need to keep them.

## Documentation

- [Configuration](./docs/configuration.md) — all config fields, cron expressions, environment variables, service recipes
- [CLI Reference](./docs/cli.md) — binary flags, service scripts, and the REST API
- [Web UI](./docs/web-ui.md) — web interface features, sub-path deployment
- [TOTP 2FA](./docs/totp.md) — two-factor authentication setup, recommended apps
- [Architecture](./docs/architecture.md) — system design and tech stack

## License

MIT
