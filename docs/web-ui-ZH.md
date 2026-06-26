# Web 管理界面

当 `WEB_ENABLE = true` 时，访问 `http://<WEB_ADDR>` 即可使用 Web 管理界面。

## 功能

### 仪表盘
- 实时服务状态，SSE（Server-Sent Events）推送更新
- 启动 / 停止 / 重启控制
- 查看每个服务的 PID、运行时间、内存占用

### 日志
- 查看 stdout 和 stderr 日志，支持标签页切换
- 清空日志
- SSE 自动刷新

### 配置
- 编辑服务配置
- 两步保存：应用（内存）→ 保存到文件（持久化）
- 创建和删除服务
- Cron 表达式验证，预览下次执行时间

### 安全
- 密码登录认证（当 `WEB_AUTH = true` 时）
- TOTP 两阶段认证（当 `TOTP_ENABLE = true` 时）

## 子目录部署

在 `[web]` 配置段中设置 `URL_PREFIX`，将界面挂载到子路径下：

```toml
[web]
WEB_ENABLE = true
URL_PREFIX = "/gorch"
```

二进制在启动时读取此配置，并在运行时将 `window.__URL_PREFIX__` 注入 SPA — 无需重新编译。

### Nginx 子目录部署

Nginx 将完整路径（包含 `/gorch/`）透传给 gorch。gorch 根据 `URL_PREFIX` 在内部去掉前缀。

```nginx
location /gorch/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header X-Forwarded-Prefix /gorch;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # SSE 支持
    proxy_http_version 1.1;
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 86400s;
}
```

### Nginx 根目录部署（对比参考）

根目录部署时不需要 `URL_PREFIX`，`proxy_pass` 直接转发即可：

```nginx
location / {
    proxy_pass http://127.0.0.1:9080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # SSE 支持
    proxy_http_version 1.1;
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 86400s;
}
```

### 工作原理

- **根目录部署**：请求 `GET /api/services` → gorch 直接响应
- **子目录部署**：请求 `GET /gorch/api/services` → gorch 通过 `URL_PREFIX` 去掉 `/gorch` 前缀 → 响应 `/api/services`
- `X-Forwarded-Prefix` 是标准头，用于告知后端反向代理的前缀；gorch 使用配置中的 `URL_PREFIX` 进行路由匹配
