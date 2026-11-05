# CLI Reference

The Odin build of `gorch` has **no subcommands**. The binary *is* the supervisor:
it loads a TOML config, starts the configured services, serves the Web UI and
REST API, and stays in the foreground until `SIGTERM`/`SIGINT` arrives — at which
point it stops every child before exiting. Service control lives in the Web UI
and the REST API; installing gorch as a system service is handled by two shell
scripts.

> **Coming from the Go version?** The Go implementation on the `lite` branch has
> `start`, `stop`, `restart`, `status`, `logs`, `install` and `uninstall`
> subcommands. None of them exist in the Odin build documented here.

## Invocation

```sh
gorch                             # foreground, config auto-detected
gorch -c /etc/gorch/gorch.toml    # explicit config
gorch --config=/etc/gorch/gorch.toml
gorch --version
gorch --help
```

| Flag | Default | Description |
|------|---------|-------------|
| `-c, --config <path>` | auto-detected | Config file path |
| `-h, --help` | | Print usage and exit |
| `-v, --version` | | Print version and exit |

There is no daemonize flag (`-d`). To run in the background, let a service
manager own the process — see [Install as a service](#install-as-a-service) —
or use your shell's job control.

### Config lookup order

Without `-c`, the first existing file wins:

1. `$GORCH_CONFIG`
2. `./gorch.toml`
3. `~/.config/gorch/gorch.toml`
4. `/etc/gorch/gorch.toml`

If none exists, gorch exits with an error. `GORCH_WEB_DIR` additionally overrides
where the SPA assets are read from (the binary has an embedded copy).

## Install as a service

Two standalone POSIX `sh` scripts replace the Go version's `install` /
`uninstall` commands. They write a service definition that runs gorch in the
foreground and let systemd/launchd supervise it.

```sh
sudo ./scripts/gorch-install --config /etc/gorch/gorch.toml   # Linux/systemd or macOS/launchd
./scripts/gorch-install --user --config ~/.config/gorch/gorch.toml
sudo ./scripts/gorch-uninstall                                # remove the service
./scripts/gorch-uninstall --user
```

A system-wide install needs root (the unit/plist lives in a system directory);
`--user` does not. `gorch-uninstall` only removes the service file — the binary,
config, logs and persisted state are kept.

Release binaries are named `gorch-<version>-<os>-<arch>`. Install one under the
plain name first, or point `--bin` at it:

```sh
sudo install -m 755 gorch-v0.4.3-linux-amd64 /usr/local/bin/gorch
```

### Script options

| `gorch-install` | Description |
|-----------------|-------------|
| `-c, --config PATH` | gorch.toml to run with (auto-detected if omitted) |
| `-b, --bin PATH` | gorch binary to run (searches `$GORCH_BIN`, `./bin/gorch`, `./gorch`, `$PATH`) |
| `-u, --user` | user-level service instead of system-wide |
| `-n, --no-start` | write the service file but do not enable/start it |
| `-d, --dry-run` | print the service file and actions, change nothing |
| `-h, --help` | show help |

| `gorch-uninstall` | Description |
|-------------------|-------------|
| `-u, --user` | remove the user-level service |
| `-d, --dry-run` | print the actions, change nothing |
| `-h, --help` | show help |

Preview before installing (nothing is written):

```sh
./scripts/gorch-install --dry-run --config /etc/gorch/gorch.toml
```

### Files the scripts write

| OS | Scope | Service file |
|----|-------|--------------|
| Linux | system | `/etc/systemd/system/gorch.service` |
| Linux | `--user` | `~/.config/systemd/user/gorch.service` |
| macOS | system | `/Library/LaunchDaemons/com.github.azhai.gorch.plist` |
| macOS | `--user` | `~/Library/LaunchAgents/com.github.azhai.gorch.plist` |

The generated definition is `gorch -c <config>` with `Type=simple` /
`Restart=on-failure` on systemd and `KeepAlive` + `RunAtLoad` on launchd. There
is no `start` verb and no `PIDFile` — the Odin binary does not fork or expose a
CLI. On systemd, output goes to the journal; on launchd it goes to
`/tmp/gorch/logs/gorch.{out,err}.log`.

`gorch-install` runs `systemctl daemon-reload && systemctl enable --now
gorch.service` (or `launchctl load -w <plist>`). If that fails, load manually:

```sh
# Linux
sudo systemctl daemon-reload
sudo systemctl enable --now gorch.service
# Linux, user scope
systemctl --user daemon-reload
systemctl --user enable --now gorch.service
# macOS
sudo launchctl load -w /Library/LaunchDaemons/com.github.azhai.gorch.plist
```

A systemd user service stops when you log out. To keep it running, also run
`loginctl enable-linger <user>`.

## Managing services

There is no `status`, `stop`, `restart` or `logs` command. Use the Web UI (see
[Web UI](./web-ui.md)) or the REST API. Mounted under `URL_PREFIX` when
configured, and protected by `WEB_AUTH` credentials when enabled:

| Action | Request |
|--------|---------|
| List services / status | `GET /api/services` |
| Single service | `GET /api/services/<name>` |
| Start / stop / restart | `POST /api/services/<name>/start\|stop\|restart` |
| Log tail | `GET /api/services/<name>/logs?type=stdout\|stderr&lines=100` |
| Clear log | `POST /api/services/<name>/logs/clear?type=stdout` |

```sh
curl -u admin:secret http://127.0.0.1:8080/api/services
curl -u admin:secret -X POST http://127.0.0.1:8080/api/services/api/restart
curl -u admin:secret 'http://127.0.0.1:8080/api/services/api/logs?type=stderr&lines=100'
```

A service without a live pid has no age: `uptime` is `null` and `startedAt` is
`0`, which the dashboard renders as a dash.

With `URL_PREFIX = "/gorch"`, prepend the prefix (`/gorch/api/services`).
