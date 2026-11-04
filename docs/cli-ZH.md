# 命令参考

Odin 版 `gorch` **没有子命令**。二进制本身就是 supervisor 进程：加载 TOML 配置、
启动配置的服务、提供 Web 界面和 REST API，然后一直前台运行，直到收到
`SIGTERM`/`SIGINT`——此时会先停掉所有子进程再退出。服务的查看与控制走 Web 界面
或 REST API；安装为系统服务则由两个独立的 shell 脚本负责。

> **从 Go 版迁移过来？** `lite` 分支上的 Go 实现带有 `start`、`stop`、`restart`、
> `status`、`logs`、`install`、`uninstall` 等子命令。这些命令在本文所述的 Odin
> 版中**全部不存在**。

## 启动方式

```sh
gorch                             # 前台运行，自动查找配置
gorch -c /etc/gorch/gorch.toml    # 显式指定配置
gorch --config=/etc/gorch/gorch.toml
gorch --version
gorch --help
```

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-c, --config <路径>` | 自动查找 | 配置文件路径 |
| `-h, --help` | | 打印用法并退出 |
| `-v, --version` | | 打印版本并退出 |

没有守护进程参数（`-d`）。要后台运行，请交给服务管理器托管（见
[安装为系统服务](#安装为系统服务)），或使用 shell 的任务控制。

### 配置文件查找顺序

未指定 `-c` 时，按以下顺序取第一个存在的文件：

1. `$GORCH_CONFIG`
2. `./gorch.toml`
3. `~/.config/gorch/gorch.toml`
4. `/etc/gorch/gorch.toml`

都不存在则报错退出。另外可用 `GORCH_WEB_DIR` 覆盖 SPA 静态资源目录（二进制内已
内嵌一份副本）。

## 安装为系统服务

两个独立的 POSIX `sh` 脚本替代了 Go 版的 `install` / `uninstall` 命令。它们生成
的服务定义让 gorch 前台运行，由 systemd/launchd 负责守护。

```sh
sudo ./scripts/gorch-install --config /etc/gorch/gorch.toml   # Linux/systemd 或 macOS/launchd
./scripts/gorch-install --user --config ~/.config/gorch/gorch.toml
sudo ./scripts/gorch-uninstall                                # 卸载服务
./scripts/gorch-uninstall --user
```

系统级安装需要 root（unit/plist 写在系统目录），`--user` 不需要。
`gorch-uninstall` **只删除服务文件**——二进制、配置、日志和状态文件都会保留。

发布产物名为 `gorch-<版本>-<系统>-<架构>`，请先安装成 `gorch` 这个普通名字，
或用 `--bin` 直接指定：

```sh
sudo install -m 755 gorch-v0.4.3-linux-amd64 /usr/local/bin/gorch
```

### 脚本参数

| `gorch-install` | 说明 |
|-----------------|------|
| `-c, --config PATH` | 使用的 gorch.toml（省略则自动查找） |
| `-b, --bin PATH` | 使用的 gorch 二进制（依次查找 `$GORCH_BIN`、`./bin/gorch`、`./gorch`、`$PATH`） |
| `-u, --user` | 安装为用户级服务，而非系统级 |
| `-n, --no-start` | 只写服务文件，不启用/启动 |
| `-d, --dry-run` | 只打印服务文件和将执行的动作，不做任何修改 |
| `-h, --help` | 显示帮助 |

| `gorch-uninstall` | 说明 |
|-------------------|------|
| `-u, --user` | 卸载用户级服务 |
| `-d, --dry-run` | 只打印将执行的动作，不做任何修改 |
| `-h, --help` | 显示帮助 |

安装前先预览（不会写入任何文件）：

```sh
./scripts/gorch-install --dry-run --config /etc/gorch/gorch.toml
```

### 脚本写入的文件

| 系统 | 范围 | 服务文件 |
|------|------|----------|
| Linux | 系统级 | `/etc/systemd/system/gorch.service` |
| Linux | `--user` | `~/.config/systemd/user/gorch.service` |
| macOS | 系统级 | `/Library/LaunchDaemons/com.github.azhai.gorch.plist` |
| macOS | `--user` | `~/Library/LaunchAgents/com.github.azhai.gorch.plist` |

生成的服务执行的是 `gorch -c <配置>`：systemd 下为 `Type=simple` +
`Restart=on-failure`，launchd 下为 `KeepAlive` + `RunAtLoad`。**没有 `start`
子命令，也没有 `PIDFile`**——Odin 二进制既不 fork，也不提供 CLI。systemd 的输出
进 journal；launchd 的输出写到 `/tmp/gorch/logs/gorch.{out,err}.log`。

`gorch-install` 会执行 `systemctl daemon-reload && systemctl enable --now
gorch.service`（macOS 为 `launchctl load -w <plist>`）。若自动启动失败，可手动加载：

```sh
# Linux
sudo systemctl daemon-reload
sudo systemctl enable --now gorch.service
# Linux 用户级
systemctl --user daemon-reload
systemctl --user enable --now gorch.service
# macOS
sudo launchctl load -w /Library/LaunchDaemons/com.github.azhai.gorch.plist
```

systemd 用户级服务在注销后会停止，如需常驻请再执行
`loginctl enable-linger <用户名>`。

## 管理服务

没有 `status`、`stop`、`restart`、`logs` 命令。请用 Web 界面（见
[Web 界面](./web-ui-ZH.md)）或 REST API。配置了 `URL_PREFIX` 时路径需要带前缀，
开启 `WEB_AUTH` 时需要带上账号密码：

| 操作 | 请求 |
|------|------|
| 服务列表 / 状态 | `GET /api/services` |
| 单个服务 | `GET /api/services/<名称>` |
| 启动 / 停止 / 重启 | `POST /api/services/<名称>/start\|stop\|restart` |
| 查看日志尾部 | `GET /api/services/<名称>/logs?type=stdout\|stderr&lines=100` |
| 清空日志 | `POST /api/services/<名称>/logs/clear?type=stdout` |

```sh
curl -u admin:secret http://127.0.0.1:8080/api/services
curl -u admin:secret -X POST http://127.0.0.1:8080/api/services/api/restart
curl -u admin:secret 'http://127.0.0.1:8080/api/services/api/logs?type=stderr&lines=100'
```

若配置了 `URL_PREFIX = "/gorch"`，请加上前缀（`/gorch/api/services`）。
