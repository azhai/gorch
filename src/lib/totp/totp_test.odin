package totp

import "core:testing"
import "core:mem"
import "core:fmt"
import "core:os"
import "core:strings"

// RFC 4226 appendix D test values for secret "12345678901234567890", 6 digits.
@(test)
test_hotp_rfc4226 :: proc(t: ^testing.T) {
	buf: [1 << 16]byte
	ar: mem.Arena
	mem.arena_init(&ar, buf[:])
	context.allocator = mem.arena_allocator(&ar)

	secret := b_slice("12345678901234567890")
	want := []string{
		"755224", "287082", "359152", "969429", "338314",
		"254676", "287922", "162583", "399871", "520489",
	}
	for idx in 0..<len(want) {
		got := hotp(secret, i64(idx), 6)
		testing.expect(t, got == want[idx], fmt.tprintf("hotp {} = {} want {}", idx, got, want[idx]))
	}
}

// base32 of the RFC secret must match the canonical encoding.
@(test)
test_base32 :: proc(t: ^testing.T) {
	buf: [1 << 16]byte
	ar: mem.Arena
	mem.arena_init(&ar, buf[:])
	context.allocator = mem.arena_allocator(&ar)

	raw := b_slice("12345678901234567890")
	enc := b32_encode(raw)
	testing.expect(t, enc == "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", enc)
	dec, ok := b32_decode(enc)
	testing.expect(t, ok)
	testing.expect(t, len(dec) == 20 && string(dec) == "12345678901234567890")
}

@(test)
test_encrypt_roundtrip :: proc(t: ^testing.T) {
	buf: [1 << 16]byte
	ar: mem.Arena
	mem.arena_init(&ar, buf[:])
	context.allocator = mem.arena_allocator(&ar)

	key: [32]u8
	for i in 0..<32 {
		key[i] = u8(i)
	}
	ok: bool
	enc, eok := encrypt_secret("JBSWY3DPEHPK3PXSECRET", key)
	ok = eok
	testing.expect(t, ok)
	dec, dok := decrypt_secret(enc, key)
	ok = dok
	testing.expect(t, ok)
	testing.expect(t, dec == "JBSWY3DPEHPK3PXSECRET")

	// wrong key must fail integrity
	other: [32]u8
	_, nok := decrypt_secret(enc, other)
	testing.expect(t, !nok)
}

@(test)
test_setup_verify_flow :: proc(t: ^testing.T) {
	buf: [1 << 18]byte
	ar: mem.Arena
	mem.arena_init(&ar, buf[:])
	context.allocator = mem.arena_allocator(&ar)

	path := "/tmp/gorch_totp_test.json"
	os.remove(path)

	// 64 hex chars -> 32 bytes master key
	hexkey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	totp_inst, err := new_totp(hexkey, "Gorch", path)
	testing.expect(t, err == .None)
	testing.expect(t, is_enabled(totp_inst))

	sr, serr := setup(totp_inst, "admin@example.com")
	testing.expect(t, serr == .None)
	testing.expect(t, len(sr.secret) > 0)
	testing.expect(t, len(sr.backup_codes) == 10)
	testing.expect(t, strings.contains(sr.otpauth_uri, "otpauth://totp/"))

	// status before enabling
	st := status(totp_inst, "admin@example.com")
	testing.expect(t, st.has_binding && !st.enabled)

	// compute a valid code for the current window
	sec_bytes, ok := b32_decode(sr.secret)
	testing.expect(t, ok)
	w := time_window()
	code := hotp(sec_bytes, w, 6)

	// Verify setup with a wrong code fails, correct code enables.
	testing.expect(t, verify_setup(totp_inst, "admin@example.com", "000000") == .Invalid_Code)
	testing.expect(t, verify_setup(totp_inst, "admin@example.com", code) == .None)
	st = status(totp_inst, "admin@example.com")
	testing.expect(t, st.enabled)

	// now verify() with a fresh window code
	w2 := time_window()
	code2 := hotp(sec_bytes, w2, 6)
	testing.expect(t, verify(totp_inst, "admin@example.com", code2) == .None)
	// replay is rejected
	testing.expect(t, verify(totp_inst, "admin@example.com", code2) == .Code_Already_Used)

	// backup code verify
	bc := sr.backup_codes[0]
	testing.expect(t, verify_backup(totp_inst, "admin@example.com", bc) == .None)
	// same backup code cannot be reused
	testing.expect(t, verify_backup(totp_inst, "admin@example.com", bc) == .Invalid_Backup_Code)

	os.remove(path)
}

@(test)
test_lockout :: proc(t: ^testing.T) {
	buf: [1 << 18]byte
	ar: mem.Arena
	mem.arena_init(&ar, buf[:])
	context.allocator = mem.arena_allocator(&ar)

	path := "/tmp/gorch_totp_lock.json"
	os.remove(path)

	hexkey := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	totp_inst, err := new_totp(hexkey, "Gorch", path)
	testing.expect(t, err == .None)

	sr, _ := setup(totp_inst, "u@example.com")
	code := "123456" // arbitrary wrong unless it happens to match
	// enable first with the real code
	sec_bytes, _ := b32_decode(sr.secret)
	real := hotp(sec_bytes, time_window(), 6)
	testing.expect(t, verify_setup(totp_inst, "u@example.com", real) == .None)

	// 5 wrong attempts lock the account
	for _ in 0..<5 {
		if code == real {
			code = "000001"
		}
		e := verify(totp_inst, "u@example.com", code)
		testing.expect(t, e == .Invalid_Code)
	}
	testing.expect(t, verify(totp_inst, "u@example.com", real) == .Account_Locked)

	os.remove(path)
}
