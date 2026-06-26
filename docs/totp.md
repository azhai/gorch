# TOTP Two-Factor Authentication

gorch supports TOTP-based two-factor authentication (2FA) for the Web UI. When enabled, logging in requires both your password and a time-based one-time password (TOTP) from your authenticator app.

Since gorch stores account credentials directly in the config file, TOTP adds a critical second layer — even if your password leaks, an attacker still can't log in without your authenticator device.

## Enabling TOTP

### 1. Generate a master secret

```sh
openssl rand -hex 32
```

This produces a 64-character hex string (32 bytes). Keep it safe — it's the encryption key for all TOTP secrets.

### 2. Update config

```toml
[web]
WEB_ENABLE = true
WEB_AUTH = true
WEB_USER = "admin"
WEB_PASS = "your-password"
TOTP_ENABLE = true
TOTP_SECRET = "your-64-char-hex-secret-here"
TOTP_DB = "auth_totp.db"
```

| Field | Description |
|-------|-------------|
| `TOTP_ENABLE` | Set to `true` to enable TOTP |
| `TOTP_SECRET` | 32-byte hex-encoded master key (64 chars) |
| `TOTP_DB` | SQLite database path for storing TOTP bindings |

### 3. Restart gorch

gorch can't restart itself — use your system's service manager:

```sh
# Linux (systemd):
sudo systemctl restart gorch

# macOS (launchd, user-level):
launchctl kickstart -k gui/$UID/com.github.azhai.gorch

# macOS (launchd, system-level):
sudo launchctl kickstart -k system/com.github.azhai.gorch
```

If running in the foreground, just `Ctrl+C` and start again.

### 4. Set up TOTP in the Web UI

1. Log in with your username and password
2. Go to **TOTP Settings** page
3. Click **Setup TOTP**
4. Scan the QR code with your authenticator app (see [Recommended Apps](#recommended-authenticator-apps) below)
5. Enter the 6-digit code from your app to verify
6. Save the backup codes in a safe place

From now on, every login requires both your password and a TOTP code.

## Recommended Authenticator Apps

### Microsoft Authenticator
- **Platforms**: iOS, Android
- **Features**: Cloud backup, multi-device sync, push notifications
- **Setup**: Tap **+** → **Other (Google, Facebook, etc.)** → Scan the QR code
- **Website**: [microsoft.com/en-us/security/mobile-authenticator-app](https://www.microsoft.com/en-us/security/mobile-authenticator-app)

### Google Authenticator
- **Platforms**: iOS, Android
- **Features**: Widely supported, simple interface, cloud sync (Google account)
- **Setup**: Tap **+** → **Scan a QR code** → Point your camera at the QR code
- **Website**: [play.google.com/store/apps/details?id=com.google.android.apps.authenticator2](https://play.google.com/store/apps/details?id=com.google.android.apps.authenticator2)

### Twilio Authy
- **Platforms**: iOS, Android, Desktop (macOS/Windows/Linux)
- **Features**: Multi-device sync, cloud backup, phone/email recovery
- **Setup**: Create an account → Tap **Add Account** → Scan the QR code
- **Website**: [authy.com](https://authy.com/)

### 2FAS Auth
- **Platforms**: iOS, Android
- **Features**: Open source, no account required, end-to-end encrypted cloud backup
- **Setup**: Tap **+** → **Scan QR code** → Point at the QR code
- **Website**: [2fas.com](https://2fas.com/)

## Backup Codes

During TOTP setup, gorch generates 10 backup codes. Store them somewhere safe (e.g., a password manager). Each code can be used **once** to log in if you lose your authenticator device.

To regenerate backup codes:
1. Go to **TOTP Settings**
2. Click **Regenerate Backup Codes**
3. Save the new codes (old codes become invalid)

## Disabling TOTP

If you need to turn off TOTP:
1. Go to **TOTP Settings**
2. Click **Disable TOTP**
3. Confirm with your password

Or remove `TOTP_ENABLE` from the config file and restart gorch using your system service manager (see step 3 above).
