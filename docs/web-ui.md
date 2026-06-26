# Web UI

When `WEB_ENABLE = true`, visit `http://<WEB_ADDR>` to use the web interface.

## Features

### Dashboard
- Real-time service status with SSE (Server-Sent Events) updates
- Start / stop / restart controls
- View PID, uptime, and memory usage per service

### Logs
- View stdout and stderr logs with tab switching
- Clear logs
- Auto-refresh via SSE

### Config
- Edit service configuration
- Two-step save: Apply (memory) → Save to File (persist)
- Create and delete services
- Cron expression validation with next-run preview

### Security
- Password-based authentication (when `WEB_AUTH = true`)
- TOTP two-factor authentication (when `TOTP_ENABLE = true`)

## Sub-path Deployment

Set `URL_PREFIX` in the `[web]` section to mount the UI under a sub-path:

```toml
[web]
WEB_ENABLE = true
URL_PREFIX = "/gorch"
```

The binary reads this at startup and injects `window.__URL_PREFIX__` into the SPA at runtime — no rebuild needed.

### Nginx (sub-path)

Nginx passes the full path (including `/gorch/`) to gorch. gorch strips the prefix internally based on `URL_PREFIX`.

```nginx
location /gorch/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header X-Forwarded-Prefix /gorch;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # SSE support
    proxy_http_version 1.1;
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 86400s;
}
```

### Nginx (root path, for comparison)

When deployed at the root, no `URL_PREFIX` needed, and `proxy_pass` is straightforward:

```nginx
location / {
    proxy_pass http://127.0.0.1:9080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # SSE support
    proxy_http_version 1.1;
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 86400s;
}
```

### How it works

- **Root deployment**: request `GET /api/services` → gorch serves it directly
- **Sub-path deployment**: request `GET /gorch/api/services` → gorch strips `/gorch` prefix (via `URL_PREFIX`) → serves `/api/services`
- `X-Forwarded-Prefix` is a standard header that tells the backend the reverse proxy prefix; gorch uses `URL_PREFIX` config for routing
