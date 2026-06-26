# 架构设计

```
CLI ──► Unix Socket IPC ──► Supervisor ──► Echo Web Server
                                   │
                                   ├── Process Manager
                                   ├── Cron Scheduler
                                   ├── Log Manager
                                   └── Status Cache
```

## 组件说明

### Supervisor
核心调度器。管理进程生命周期、Cron 定时调度和状态追踪。

### Process Manager（进程管理器）
启动、停止和监控各个进程。处理重启策略、退避延迟和守护进程跟踪。

### Cron Scheduler（定时调度器）
6 位 cron 表达式解析和调度器。按计划触发服务执行，检测并跳过重叠执行。

### Log Manager（日志管理器）
日志文件管理 — 读取、清空、清理。属于 `gobus/log` 共享库的一部分。

### Status Cache（状态缓存）
缓存服务状态快照，用于快速查询和 SSE 更新推送。

### IPC Layer（进程间通信层）
基于 Unix Socket 的进程间通信。CLI 命令通过 `/var/run/gorch.sock` 与运行中的 supervisor 通信。

### Web Server（Web 服务器）
基于 Echo 的 HTTP 服务器，内置 React SPA。提供 REST API 和实时 SSE 更新。

## 技术栈

- **Go** — 核心运行时
- **Cobra** — CLI 框架
- **Echo** — HTTP 服务器
- **go-toml/v2** — TOML 解析
- **React + TypeScript + Tailwind** — Web 界面（通过 embed 嵌入）
- **gobus/log** — 日志管理
- **go-totp** — TOTP 两阶段认证
