# 架构设计

`gorch` 是一个前台运行的 Odin 单二进制程序：加载 TOML 配置、启动被管理的服务、执行重启策略与 cron 调度，并通过 HTTP 提供 Web 界面和全部控制 API。没有 CLI 服务端、没有 Unix socket、也没有辅助二进制——所有逻辑都在同一个进程内，由 systemd / launchd 通过 `scripts/gorch-install` 监管。

```
gorch.toml ──► config / toml ──► Supervisor ──► 被管理服务
                                     │  ├── 重启策略 + 退避
                                     │  ├── cron 调度
                                     │  ├── RSS 内存采样
                                     │  └── stdout / stderr 日志轮转
                                     │
                                     ├── state（oboe cache → gorch.state）
                                     └── 状态快照
                                                  │
                        core:net HTTP 服务端 ────┴──► 浏览器
                          ├── 内嵌 SPA
                          ├── REST API + SSE 推送
                          └── JWT 登录 / TOTP 2FA
```

## 组件说明

### Supervisor（`src/lib/supervisor`）

主循环：启动与回收进程、执行重启策略和退避、触发 cron 服务、轮转日志，并每隔几秒采样各服务的常驻内存（按整棵进程树求和）。内核读取（Linux 走 `/proc`，macOS 走 libproc）在 supervisor 锁之外进行，慢读不会阻塞 API。

### 平台层（`proc_start_*.odin`、`proc_rss_*.odin`）

Linux 遍历 `/proc`，macOS 调用 libproc。进程启动时间还用于识别 pid 复用，避免重启后接管到过期的 pid。

### Cron（`src/lib/cron`）

6 位 cron 表达式解析器，自动跳过重叠执行。

### 状态与快照（`src/lib/state`、`src/lib/status`）

`state` 把 pid、退出码、重启次数存在 `oboe:cache` 中，原子写入 `gorch.state`——不用数据库，也没有 pid 文件。`status` 定义提供给仪表盘的状态记录。

### 配置与解析器（`src/lib/config`、`toml`、`json`）

自写的 TOML 与 JSON 解析器。JSON 采用游标式读取：现场消费的值借用请求体，需要持久化的结构则使用 owning cursor 克隆字符串（请求体位于连接栈上，直接切片会悬挂）。

### 日志（`src/lib/logfile`、`log`）

每个服务独立的 stdout / stderr 文件，按时间轮转。

### Web 层（`src/lib/web`、`web/`）

基于 `core:net` 的 HTTP 服务端，每个连接独占一个线程，因此长连接的 SSE 流不会阻塞仪表盘。负责提供 SPA、为子目录部署注入 `window.__URL_PREFIX__`、暴露 REST API 与 SSE，以及 JWT 登录和 TOTP 两阶段认证。

前端是原生 ES 模块加 vendor 的 Alpine，无需打包。`tools/embedgen` 把 `web/` 压成单个裸 deflate blob（`assets.bin`）和一张索引表，服务端启动时解压一次。配置旁的 `web/` 目录按文件覆盖内嵌副本，改前端无需重新构建。

### 认证（`src/lib/jwt`、`totp`、`qr`）

可选的 HTTP 认证与 TOTP 两阶段认证，内置二维码生成。

## 目录结构

| 路径 | 内容 |
|------|------|
| `src/cmd/gorch` | `package main` — 唯一入口（`-c/-h/-v`） |
| `src/lib/supervisor` | 进程生命周期、cron 触发、重启策略、RSS 采样 |
| `src/lib/web` | HTTP 传输、静态资源、REST API、SSE、认证接线 |
| `src/lib/config`、`toml`、`json` | 配置结构与自写解析器 |
| `src/lib/state`、`status` | oboe cache 快照与状态记录 |
| `src/lib/cron`、`logfile`、`log` | cron 表达式、日志轮转、内部日志 |
| `src/lib/jwt`、`totp`、`qr`、`paths` | 认证、2FA、默认路径 |
| `web/` | Alpine SPA（Material 3 Monet）；直接静态提供，无需打包 |
| `tools/embedgen` | 把 `web/` 打包成 `src/lib/web/assets.bin` + `assets_gen.odin` |
| `scripts/` | systemd / launchd 用的 `gorch-install`、`gorch-uninstall` |
| `vendor/oboe` | vendor 子模块 — 提供 `oboe:cache` 状态存储 |
| `third_party/zlib` | 精简 zlib 源码，仅用于 Linux 静态链接 |

## 构建

`make one` 构建本机版本；`make release` 构建四平台矩阵（darwin/linux × arm64/amd64），并额外用 UPX 压缩两个 Linux 二进制。macOS 原生链接（macOS 11+）；Linux 先编译成 object，再由 `zig cc` 静态链接 musl 与 vendor 的 zlib。发布构建使用 `-o:size` 并剥离符号，`VERSION` 来自 `git describe`。

## 技术栈

- **Odin**（`dev-2026-10` 或更新）— 全部实现
- **Odin core** — `core:net`、`os`、`sync`、`thread`、`time`、`compress/zlib`、`crypto*`、posix/darwin 系统调用
- **oboe:cache** — 压缩且原子快照的状态存储（vendor 子模块）
- **zlib** — vendor 的精简源码，用于 Linux 静态链接
- **Alpine.js 3 + Material 3（Monet）+ ES modules** — Web 界面，无构建步骤
- **UPX** — 可选，用 `--brute` 压缩 Linux 发布产物
- **systemd / launchd** — 通过 `scripts/gorch-*` 监管服务
