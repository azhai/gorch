# 命令参考

## 命令列表

| 命令 | 说明 |
|------|------|
| `gorch start [-c 配置] [-d] [-s 名称]` | 启动服务（`-d` 以守护进程运行） |
| `gorch stop [-s 名称]` | 停止全部或指定服务 |
| `gorch restart -s <名称>` | 重启指定服务 |
| `gorch status [-s 名称] [-j] [-l]` | 查看状态（`-j` JSON 输出，`-l` 实时刷新） |
| `gorch logs -s <名称> [-n 行数] [-f] [-P]` | 查看服务日志（`-f` 跟踪，`-P` 清空） |
| `gorch install [--user]` | 安装为系统服务 |
| `gorch uninstall [--user]` | 卸载系统服务 |

## 全局参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-c, --config` | `gorch.toml` | 配置文件路径 |

## 命令详解

### start

```sh
gorch start                    # 前台运行，全部服务
gorch start -d                 # 守护进程方式（后台）
gorch start -c /etc/gorch.toml # 指定配置文件
gorch start -s api             # 启动单个服务
```

### stop

```sh
gorch stop                     # 停止全部服务
gorch stop -s api              # 停止指定服务
```

### restart

```sh
gorch restart -s api           # 重启服务
```

如果服务配置了 `RESTART_CMD`，会执行优雅重载而非 stop+start。

### status

```sh
gorch status                   # 全部服务
gorch status -s api            # 单个服务
gorch status -j                # JSON 输出
gorch status -l                # 实时刷新（1 秒间隔）
```

### logs

```sh
gorch logs -s api              # 最近 100 行
gorch logs -s api -n 500      # 最近 500 行
gorch logs -s api -f          # 实时跟踪（tail -f）
gorch logs -P                 # 清空所有日志文件
```

### install / uninstall

```sh
gorch install                  # 系统级安装（Linux: systemd, macOS: launchd）
gorch install --user           # 用户级安装
gorch uninstall                # 卸载系统服务
gorch uninstall --user         # 卸载用户服务
```

`gorch install` 会自动写入服务文件、加载并启动。如果自动启动失败，可手动加载：

```sh
# macOS:
launchctl load -w ~/Library/LaunchAgents/com.github.azhai.gorch.plist
# Linux:
systemctl daemon-reload
systemctl enable --now gorch
```
