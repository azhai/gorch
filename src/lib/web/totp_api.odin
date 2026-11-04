package web

// Second factor.
//
// The store lives in src/lib/totp; these handlers only translate HTTP. Setup is
// deliberately two-step: `setup` mints a secret and the backup codes but leaves
// the binding disabled until `verify-setup` sees a code generated from that
// secret, so a botched enrolment cannot lock the operator out of the daemon.
//
// The identity is the configured WEB_USER. gorch has exactly one account, and
// taking the name from the request would let anyone enrol a second factor onto
// someone else's binding.

import "core:fmt"
import "core:net"
import "core:strings"

import "../../lib/qr"
import "../../lib/totp"

handle_totp :: proc(srv: ^Server, conn: net.TCP_Socket, method, path, raw: string) {
	action := strings.trim_prefix(path, "/api/totp/")
	user := srv.cfg.web.web_user
	t := srv.provider.totp

	if t == nil {
		// Without a store the Security page still has to render, so `status`
		// answers "not configured" instead of an error: the operator needs to
		// see why nothing can be enrolled.
		if action == "status" && method == "GET" {
			send_json_ok(
				conn,
				200,
				`{"enabled":false,"backupCodes":0,"hasBinding":false,"configured":false}`,
			)
			return
		}
		send_json_error(
			conn,
			503,
			"TOTP is not configured: set TOTP_ENABLE and a 32-byte hex TOTP_SECRET",
		)
		return
	}

	switch action {
	case "status":
		if method != "GET" {
			send_json_error(conn, 405, "method not allowed")
			return
		}
		st := totp.status(t, user)
		b := strings.builder_make()
		fmt.sbprintf(
			&b,
			`{{"enabled":{},"backupCodes":{},"hasBinding":{},"configured":true}}`,
			st.enabled,
			st.backup_codes,
			st.has_binding,
		)
		send_json_ok(conn, 200, strings.to_string(b))

	case "setup":
		if method != "POST" {
			send_json_error(conn, 405, "method not allowed")
			return
		}
		res, err := totp.setup(t, user)
		if code, msg := totp_error(err); code != 0 {
			send_json_error(conn, code, msg)
			return
		}
		b := strings.builder_make()
		strings.write_string(&b, `{"secret":`)
		write_json_string(&b, res.secret)
		strings.write_string(&b, `,"otpauthUri":`)
		write_json_string(&b, res.otpauth_uri)

		// The QR image is drawn here rather than in the browser, so the client
		// needs no QR library. Encoding the URI cannot fail in practice; if it
		// ever does (an unreasonably long user name overflows the symbol), the
		// field is simply absent and the page falls back to the manual secret.
		if m, qr_err := qr.encode(res.otpauth_uri); qr_err == .None {
			strings.write_string(&b, `,"qrCode":`)
			uri := qr.data_uri(m, 6)
			write_json_string(&b, uri)
			delete(uri)
			qr.destroy(&m)
		}

		strings.write_string(&b, `,"backupCodes":[`)
		for c, i in res.backup_codes {
			if i > 0 {
				strings.write_byte(&b, ',')
			}
			write_json_string(&b, c)
		}
		strings.write_string(&b, `]}`)
		send_json_ok(conn, 200, strings.to_string(b))

	case "verify-setup":
		if method != "POST" {
			send_json_error(conn, 405, "method not allowed")
			return
		}
		code, _ := json_string_field(request_body(raw), "code")
		err := totp.verify_setup(t, user, strings.trim_space(code))
		if ec, msg := totp_error(err); ec != 0 {
			send_json_error(conn, ec, msg)
			return
		}
		send_json_ok(conn, 200, `{"enabled":true}`)

	case "disable":
		if method != "POST" {
			send_json_error(conn, 405, "method not allowed")
			return
		}
		err := totp.disable(t, user)
		if ec, msg := totp_error(err); ec != 0 {
			send_json_error(conn, ec, msg)
			return
		}
		send_json_ok(conn, 200, `{"enabled":false}`)

	case "regenerate-backup-codes":
		if method != "POST" {
			send_json_error(conn, 405, "method not allowed")
			return
		}
		codes, err := totp.regenerate_backup_codes(t, user)
		if ec, msg := totp_error(err); ec != 0 {
			send_json_error(conn, ec, msg)
			return
		}
		b := strings.builder_make()
		strings.write_string(&b, `{"backupCodes":[`)
		for c, i in codes {
			if i > 0 {
				strings.write_byte(&b, ',')
			}
			write_json_string(&b, c)
		}
		strings.write_string(&b, `]}`)
		send_json_ok(conn, 200, strings.to_string(b))

	case:
		send_json_error(conn, 404, "not found")
	}
}

// totp_error maps the library's sentinels onto HTTP. A zero status means the
// call succeeded and the caller should answer normally.
totp_error :: proc(err: totp.Error) -> (status: int, message: string) {
	switch err {
	case .None:
		return 0, ""
	case .Not_Setup:
		return 400, "TOTP has not been set up"
	case .Not_Enabled:
		return 400, "TOTP is not enabled"
	case .Invalid_Code:
		return 400, "invalid verification code"
	case .Account_Locked:
		return 429, "too many failed attempts, try again in 15 minutes"
	case .Code_Already_Used:
		return 400, "that code was already used"
	case .Invalid_Backup_Code:
		return 400, "invalid backup code"
	case .Bad_Master_Key:
		return 500, "TOTP_SECRET must be 32 hex characters (openssl rand -hex 32)"
	case .Crypto_Failure:
		return 500, "cannot decrypt the stored secret"
	case .Storage_Failure:
		return 500, "cannot write the TOTP database"
	}
	return 500, "unknown TOTP failure"
}
