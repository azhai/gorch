# TOTP 两阶段认证

gorch 支持基于 TOTP 的两阶段认证（2FA）用于 Web 管理界面。启用后，登录需要同时输入密码和身份验证器 App 生成的动态口令（TOTP）。

由于 gorch 的账号密码直接写在配置文件里，TOTP 提供了关键的第二层保护 — 就算密码泄露了，没有你的身份验证器设备也无法登录。

## 启用 TOTP

### 1. 生成主密钥

```sh
openssl rand -hex 32
```

会生成一个 64 字符的十六进制字符串（32 字节）。请妥善保管 — 它是所有 TOTP 密钥的加密密钥。

### 2. 更新配置文件

```toml
[web]
WEB_ENABLE = true
WEB_AUTH = true
WEB_USER = "admin"
WEB_PASS = "your-password"
TOTP_ENABLE = true
TOTP_SECRET = "你的64位十六进制密钥"
TOTP_DB = "auth_totp.db"
```

| 字段 | 说明 |
|------|------|
| `TOTP_ENABLE` | 设为 `true` 启用 TOTP |
| `TOTP_SECRET` | 32 字节十六进制主密钥（64 个字符） |
| `TOTP_DB` | 存储 TOTP 绑定信息的 SQLite 数据库路径 |

### 3. 重启 gorch

gorch 不能自己重启自己 — 请使用系统的服务管理器：

```sh
# Linux（systemd）：
sudo systemctl restart gorch

# macOS（launchd，用户级）：
launchctl kickstart -k gui/$UID/com.github.azhai.gorch

# macOS（launchd，系统级）：
sudo launchctl kickstart -k system/com.github.azhai.gorch
```

如果是前台运行的，直接 `Ctrl+C` 停止后再启动即可。

### 4. 在 Web 界面中设置 TOTP

1. 使用用户名和密码登录
2. 进入 **TOTP 设置** 页面
3. 点击 **设置 TOTP**
4. 用你的身份验证器 App 扫描二维码（见下方[推荐 APP](#推荐的身份验证器-app)）
5. 输入 App 中显示的 6 位验证码进行确认
6. 将备用码保存到安全的地方

从现在开始，每次登录都需要输入密码和 TOTP 验证码。

## 推荐的身份验证器 APP

### Microsoft Authenticator
- **平台**：iOS、Android
- **特点**：云备份、多设备同步、推送通知
- **设置方法**：点击 **+** → **其他（Google、Facebook 等）** → 扫描二维码
- **官网**：[microsoft.com/zh-cn/security/mobile-authenticator-app](https://www.microsoft.com/zh-cn/security/mobile-authenticator-app)

### Google Authenticator
- **平台**：iOS、Android
- **特点**：广泛支持、界面简洁、云同步（Google 账号）
- **设置方法**：点击 **+** → **扫描二维码** → 用摄像头对准二维码
- **官网**：[play.google.com/store/apps/details?id=com.google.android.apps.authenticator2](https://play.google.com/store/apps/details?id=com.google.android.apps.authenticator2)

### Twilio Authy
- **平台**：iOS、Android、桌面端（macOS/Windows/Linux）
- **特点**：多设备同步、云备份、手机/邮箱恢复
- **设置方法**：创建账号 → 点击 **添加账号** → 扫描二维码
- **官网**：[authy.com](https://authy.com/)

### 2FAS Auth
- **平台**：iOS、Android
- **特点**：开源、无需注册账号、端到端加密云备份
- **设置方法**：点击 **+** → **扫描二维码** → 对准二维码
- **官网**：[2fas.com](https://2fas.com/)

## 备用码

在 TOTP 设置过程中，gorch 会生成 10 个备用码。请将它们存放在安全的地方（如密码管理器）。每个备用码只能使用**一次**，用于在丢失验证器设备时登录。

重新生成备用码：
1. 进入 **TOTP 设置**
2. 点击 **重新生成备用码**
3. 保存新的备用码（旧码失效）

## 关闭 TOTP

如果需要关闭 TOTP：
1. 进入 **TOTP 设置**
2. 点击 **禁用 TOTP**
3. 用密码确认

或者从配置文件中移除 `TOTP_ENABLE`，然后用系统服务管理器重启 gorch（见第 3 步）。
