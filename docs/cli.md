# CLI Reference

## Commands

| Command | Description |
|---------|-------------|
| `gorch start [-c config] [-d] [-s name]` | Start services (`-d` to daemonize) |
| `gorch stop [-s name]` | Stop all or specific service |
| `gorch restart -s <name>` | Restart a service |
| `gorch status [-s name] [-j] [-l]` | Show status (`-j` for JSON, `-l` for live refresh) |
| `gorch logs -s <name> [-n lines] [-f] [-P]` | View service logs (`-f` follow, `-P` purge) |
| `gorch install [--user]` | Install as system service |
| `gorch uninstall [--user]` | Uninstall system service |

## Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-c, --config` | `gorch.toml` | Config file path |

## Command Details

### start

```sh
gorch start                    # foreground, all services
gorch start -d                 # daemonize (background)
gorch start -c /etc/gorch.toml # specify config
gorch start -s api             # start single service
```

### stop

```sh
gorch stop                     # stop all services
gorch stop -s api              # stop specific service
```

### restart

```sh
gorch restart -s api           # restart service
```

If the service has `RESTART_CMD` set, a graceful reload is performed instead of stop+start.

### status

```sh
gorch status                   # all services
gorch status -s api            # single service
gorch status -j                # JSON output
gorch status -l                # live refresh (1s interval)
```

### logs

```sh
gorch logs -s api              # last 100 lines
gorch logs -s api -n 500      # last 500 lines
gorch logs -s api -f          # follow (tail -f)
gorch logs -P                 # purge all log files
```

### install / uninstall

```sh
gorch install                  # system-wide (Linux: systemd, macOS: launchd)
gorch install --user           # user-level service
gorch uninstall                # remove system service
gorch uninstall --user         # remove user service
```

`gorch install` automatically writes the service file, loads and starts it. If auto-start fails, load manually:

```sh
# macOS:
launchctl load -w ~/Library/LaunchAgents/com.github.azhai.gorch.plist
# Linux:
systemctl daemon-reload
systemctl enable --now gorch
```
