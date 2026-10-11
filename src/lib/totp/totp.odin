package totp

import "core:fmt"
import "core:os"
import "core:mem"
import "core:time"
import "core:sync"
import "core:strings"
import "core:crypto"
import "core:crypto/hmac"
import "core:crypto/hash"
import "core:encoding/base64"
import "core:encoding/hex"
import "core:encoding/json"

// bstr returns an owned copy of a byte slice reinterpreted as a string.
bstr :: proc(b: []byte) -> string {
	c := make([]byte, len(b))
	if len(b) > 0 {
		mem.copy(raw_data(c), raw_data(b), len(b))
	}
	return string(c)
}

hx_encode :: proc(b: []byte) -> string {
	raw := hex.encode(b)
	return bstr(raw)
}

hx_decode :: proc(s: string) -> ([]byte, bool) {
	return hex.decode(mem.byte_slice(raw_data(s), len(s)))
}

// bcopy copies src into dst (byte slices), bounded by the shorter length.
bcopy :: proc(dst, src: []byte) {
	n := len(dst)
	if len(src) < n {
		n = len(src)
	}
	if n > 0 {
		mem.copy(raw_data(dst), raw_data(src), n)
	}
}

// xor_u8 computes a bitwise XOR; Odin has no binary ^ operator.
xor_u8 :: proc(a, b: u8) -> u8 {
	return (a | b) & ~(a & b)
}

// Errors mirror the go-totp error sentinels; the server maps them to messages.
Error :: enum {
	None,
	Not_Setup,
	Not_Enabled,
	Invalid_Code,
	Account_Locked,
	Code_Already_Used,
	Invalid_Backup_Code,
	Bad_Master_Key,
	Crypto_Failure,
	Storage_Failure,
}

// Binding mirrors models.Binding (SQLite) collapsed onto a JSON field set.
Binding :: struct {
	user_id:         string,
	secret_enc:      string,
	enabled:         bool,
	failed_attempts: int,
	locked_until:    i64, // unix seconds; 0 == unlocked
	created_at:      i64,
	updated_at:      i64,
}

BackupCode :: struct {
	code_hash: string,
	used:      bool,
}

UsedCode :: struct {
	time_window: i64,
	code:        string,
	used_at:     i64,
}

// DB is the on-disk (JSON) representation of all TOTP state.
DB :: struct {
	bindings: map[string]Binding,
	backups:  map[string][]BackupCode,
	used:     map[string][]UsedCode,
}

TOTP :: struct {
	master_key: [32]u8,
	issuer:     string,
	enabled:    bool,
	path:       string,
	mu:         sync.RW_Mutex,
	db:         DB,
}

SetupResult :: struct {
	secret:       string,
	otpauth_uri:  string,
	backup_codes: []string,
}

StatusResult :: struct {
	enabled:      bool,
	backup_codes: int,
	has_binding:  bool,
}

// ---- hashing helpers ----

hmac_sha256 :: proc(msg, key: []byte) -> [32]u8 {
	dst: [32]u8
	hmac.sum(.SHA256, dst[:], msg, key)
	return dst
}

hmac_sha1 :: proc(msg, key: []byte) -> [20]u8 {
	dst: [20]u8
	hmac.sum(.Insecure_SHA1, dst[:], msg, key)
	return dst
}

// ---- base32 (RFC 4648, uppercase, no padding) ----

b32_alphabet := [32]u8{'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
	'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
	'2', '3', '4', '5', '6', '7'}

b32_char_val :: proc(c: u8) -> int {
	if c >= 'A' && c <= 'Z' {
		return int(c - 'A')
	}
	if c >= '2' && c <= '7' {
		return int(c - '2') + 26
	}
	return -1
}

b32_encode :: proc(data: []byte) -> string {
	out := [dynamic]u8{}
	buf := u32(0)
	bits := 0
	for b in data {
		buf = (buf << 8) | u32(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			idx := int((buf >> u32(bits)) & 0x1f)
			append(&out, b32_alphabet[idx])
		}
		buf &= (1 << u32(bits)) - 1
	}
	if bits > 0 {
		idx := int((buf << u32(5 - bits)) & 0x1f)
		append(&out, b32_alphabet[idx])
	}
	res := bstr(out[:])
	delete(out)
	return res
}

b32_decode :: proc(s: string) -> ([]byte, bool) {
	buf := u32(0)
	bits := 0
	out := [dynamic]u8{}
	for c in s {
		if c == '=' || c == ' ' || c == '-' {
			continue
		}
		up := u8(c)
		if up >= 'a' && up <= 'z' {
			up -= 32
		}
		v := b32_char_val(up)
		if v < 0 {
			delete(out)
			return nil, false
		}
		buf = (buf << 5) | u32(v)
		bits += 5
		if bits >= 8 {
			bits -= 8
			append(&out, u8((buf >> u32(bits)) & 0xff))
			buf &= (1 << u32(bits)) - 1
		}
	}
	// leftover bits (0,3,5,7 at a time) must be zero padding; >=5 means a
	// dangling partial character which is invalid.
	if bits >= 8 {
		delete(out)
		return nil, false
	}
	if buf != 0 {
		delete(out)
		return nil, false
	}
	res := make([]byte, len(out))
	if len(out) > 0 {
		mem.copy(raw_data(res), raw_data(out[:]), len(out))
	}
	delete(out)
	return res, true
}

// ---- HOTP / TOTP (RFC 4226 / 6238) ----

hotp :: proc(secret: []byte, counter: i64, digits: int) -> string {
	buf: [8]u8
	u := u64(counter)
	for i in 0..<8 {
		buf[7-i] = u8((u >> u32(8*i)) & 0xff)
	}
	digest := hmac_sha1(buf[:], secret)
	offset := digest[19] & 0x0f
	bin := (u32(digest[offset]&0x7f) << 24) |
		(u32(digest[offset+1]) << 16) |
		(u32(digest[offset+2]) << 8) |
		u32(digest[offset+3])
	mod := u32(1)
	for _ in 0..<digits {
		mod *= 10
	}
	code := bin % mod
	// zero pad to `digits`
	s := fmt.tprintf("{}", code)
	for len(s) < digits {
		s = fmt.tprintf("0{}", s)
	}
	return s
}

time_window :: proc() -> i64 {
	return time.to_unix_seconds(time.now()) / 30
}

// verify_totp checks a 6-digit code against the base32 secret with skew +/-1.
verify_totp :: proc(secret_b32, code: string) -> (valid: bool, window: i64) {
	if len(code) != 6 {
		return false, 0
	}
	for c in code {
		if c < '0' || c > '9' {
			return false, 0
		}
	}
	secret, ok := b32_decode(secret_b32)
	if !ok {
		return false, 0
	}
	cur := time_window()
	for skew in -1..=1 {
		if hotp(secret, cur + i64(skew), 6) == code {
			return true, cur
		}
	}
	return false, 0
}

generate_secret :: proc() -> string {
	raw: [20]u8
	crypto.rand_bytes(raw[:])
	return b32_encode(raw[:])
}

generate_otpauth_uri :: proc(secret, user, issuer: string) -> string {
	return fmt.tprintf("otpauth://totp/{}:{}?secret={}&issuer={}", issuer, user, secret, issuer)
}

// ---- encryption (HMAC-SHA256 keystream XOR + HMAC integrity) ----

keystream :: proc(key: [32]u8, salt: []byte, n: int) -> []byte {
	kk := key
	out := make([]byte, n)
	counter: u32 = 0
	pos := 0
	for pos < n {
		block_input := make([]byte, len(salt)+4)
		bcopy(block_input, salt)
		block_input[len(salt)+0] = u8((counter >> 24) & 0xff)
		block_input[len(salt)+1] = u8((counter >> 16) & 0xff)
		block_input[len(salt)+2] = u8((counter >> 8) & 0xff)
		block_input[len(salt)+3] = u8(counter & 0xff)
		block := hmac_sha256(block_input, kk[:])
		take := 32
		if n-pos < take {
			take = n - pos
		}
		bcopy(out[pos:pos+take], (&block)[:take])
		delete(block_input)
		pos += take
		counter += 1
	}
	return out
}

b_slice :: proc(s: string) -> []byte {
	return mem.byte_slice(raw_data(s), len(s))
}

encrypt_secret :: proc(plain: string, key: [32]u8) -> (string, bool) {
	kk := key
	salt: [16]u8
	crypto.rand_bytes(salt[:])
	pb := b_slice(plain)
	ks := keystream(key, salt[:], len(pb))
	ct := make([]byte, len(pb))
	for i in 0..<len(pb) {
		ct[i] = xor_u8(pb[i], ks[i])
	}
	delete(ks)
	mac_input := make([]byte, len(salt)+len(ct))
	bcopy(mac_input, salt[:])
	bcopy(mac_input[len(salt):], ct)
	mac := hmac_sha256(mac_input, kk[:])
	delete(mac_input)
	combined := [dynamic]u8{}
	reserve(&combined, len(salt)+32+len(ct))
	for b in salt {
		append(&combined, b)
	}
	for i in 0..<32 {
		append(&combined, mac[i])
	}
	for b in ct {
		append(&combined, b)
	}
	res, err := base64.encode(combined[:])
	delete(combined)
	if err != nil {
		return "", false
	}
	return res, true
}

decrypt_secret :: proc(b64: string, key: [32]u8) -> (string, bool) {
	kk := key
	combined, err := base64.decode(b64)
	if err != nil {
		return "", false
	}
	defer delete(combined)
	if len(combined) < 48 {
		return "", false
	}
	salt := combined[0:16]
	mac := combined[16:48]
	ct := combined[48:]
	mac_input := make([]byte, len(salt)+len(ct))
	bcopy(mac_input, salt)
	bcopy(mac_input[len(salt):], ct)
	want := hmac_sha256(mac_input, kk[:])
	delete(mac_input)
	if crypto.compare_constant_time(mac, (&want)[:]) != 1 {
		return "", false
	}
	ks := keystream(key, salt, len(ct))
	plain := make([]byte, len(ct))
	for i in 0..<len(ct) {
		plain[i] = xor_u8(ct[i], ks[i])
	}
	delete(ks)
	res := bstr(plain)
	delete(plain)
	return res, true
}

// ---- backup codes ----

generate_backup_code :: proc() -> string {
	b: [4]u8
	crypto.rand_bytes(b[:])
	h := bstr(hex.encode_upper(b[:]))
	return fmt.tprintf("{}-{}", h[0:4], h[4:8])
}

generate_backup_codes :: proc(count: int) -> []string {
	seen := make(map[string]bool)
	out := [dynamic]string{}
	for len(out) < count {
		c := generate_backup_code()
		if _, dup := seen[c]; !dup {
			seen[c] = true
			append(&out, c)
		}
	}
	delete(seen)
	res := make([]string, len(out))
	for i in 0..<len(out) {
		res[i] = out[i]
	}
	delete(out)
	return res
}

hash_backup_code :: proc(code: string) -> string {
	stripped, _ := strings.replace_all(code, "-", "")
	norm := strings.to_upper(stripped)
	h := hmac_sha256(b_slice(norm), b_slice("backup"))
	return hx_encode(h[:])
}

// ---- persistence ----

empty_db :: proc() -> DB {
	db: DB
	db.bindings = make(map[string]Binding)
	db.backups = make(map[string][]BackupCode)
	db.used = make(map[string][]UsedCode)
	return db
}

load_db :: proc(path: string) -> DB {
	data, err := os.read_entire_file(path, context.allocator)
	if err != nil {
		return empty_db()
	}
	defer delete(data)
	db: DB
	if e := json.unmarshal(data, &db); e != nil {
		return empty_db()
	}
	if db.bindings == nil {
		db.bindings = make(map[string]Binding)
	}
	if db.backups == nil {
		db.backups = make(map[string][]BackupCode)
	}
	if db.used == nil {
		db.used = make(map[string][]UsedCode)
	}
	return db
}

save_db :: proc(t: ^TOTP) -> bool {
	blob, e := json.marshal(t.db)
	if e != nil {
		return false
	}
	defer delete(blob)
	perm := os.Permissions{.Read_User, .Write_User}
	if os.write_entire_file_from_bytes(t.path, blob, perm) != nil {
		return false
	}
	return true
}

// ---- constructor ----

new_totp :: proc(secret_hex, issuer, path: string) -> (^TOTP, Error) {
	key_bytes, ok := hx_decode(secret_hex)
	if !ok || len(key_bytes) != 32 {
		return nil, .Bad_Master_Key
	}
	t := new(TOTP)
	bcopy(t.master_key[:], key_bytes)
	delete(key_bytes)
	t.issuer = strings.clone(issuer)
	t.path = strings.clone(path)
	t.enabled = true
	t.db = load_db(path)
	return t, .None
}

is_enabled :: proc(t: ^TOTP) -> bool {
	return t != nil && t.enabled
}

issuer :: proc(t: ^TOTP) -> string {
	if t == nil {
		return ""
	}
	return t.issuer
}

// get_binding returns a copy of the stored binding.
get_binding :: proc(t: ^TOTP, user: string) -> (Binding, bool) {
	sync.rw_mutex_shared_lock(&t.mu)
	defer sync.rw_mutex_shared_unlock(&t.mu)
	b, ok := t.db.bindings[user]
	return b, ok
}

decrypt :: proc(t: ^TOTP, b: Binding) -> (string, bool) {
	return decrypt_secret(b.secret_enc, t.master_key)
}

setup :: proc(t: ^TOTP, user: string) -> (SetupResult, Error) {
	secret := generate_secret()
	enc, ok := encrypt_secret(secret, t.master_key)
	if !ok {
		return SetupResult{}, .Crypto_Failure
	}
	codes := generate_backup_codes(10)
	hashes := [dynamic]string{}
	for c in codes {
		append(&hashes, hash_backup_code(c))
	}
	now := time.to_unix_seconds(time.now())

	sync.rw_mutex_lock(&t.mu)
	b := Binding{
		user_id    = strings.clone(user),
		secret_enc = strings.clone(enc),
		enabled    = false,
		created_at = now,
		updated_at = now,
	}
	t.db.bindings[user] = b
	bcs := [dynamic]BackupCode{}
	for h in hashes {
		append(&bcs, BackupCode{code_hash = strings.clone(h), used = false})
	}
	t.db.backups[user] = bcs[:]
	ok_save := save_db(t)
	sync.rw_mutex_unlock(&t.mu)
	delete(hashes)
	if !ok_save {
		return SetupResult{}, .Storage_Failure
	}
	return SetupResult{
		secret       = secret,
		otpauth_uri  = generate_otpauth_uri(secret, user, t.issuer),
		backup_codes = codes,
	}, .None
}

verify_setup :: proc(t: ^TOTP, user, code: string) -> Error {
	b, ok := get_binding(t, user)
	if !ok {
		return .Not_Setup
	}
	secret, dok := decrypt(t, b)
	if !dok {
		return .Crypto_Failure
	}
	valid, _ := verify_totp(secret, code)
	if !valid {
		return .Invalid_Code
	}
	sync.rw_mutex_lock(&t.mu)
	b.enabled = true
	b.updated_at = time.to_unix_seconds(time.now())
	t.db.bindings[user] = b
	ok_save := save_db(t)
	sync.rw_mutex_unlock(&t.mu)
	if !ok_save {
		return .Storage_Failure
	}
	return .None
}

update_failed_attempts :: proc(t: ^TOTP, user: string, attempts: int, locked_until: i64) {
	sync.rw_mutex_lock(&t.mu)
	if b, ok := t.db.bindings[user]; ok {
		b.failed_attempts = attempts
		b.locked_until = locked_until
		b.updated_at = time.to_unix_seconds(time.now())
		t.db.bindings[user] = b
		save_db(t)
	}
	sync.rw_mutex_unlock(&t.mu)
}

is_code_used :: proc(t: ^TOTP, user: string, window: i64, code: string) -> bool {
	sync.rw_mutex_shared_lock(&t.mu)
	defer sync.rw_mutex_shared_unlock(&t.mu)
	list := t.db.used[user]
	for uc in list {
		if uc.time_window == window && uc.code == code {
			return true
		}
	}
	return false
}

save_used_code :: proc(t: ^TOTP, user: string, window: i64, code: string) {
	sync.rw_mutex_lock(&t.mu)
	// prune entries older than 90s (window granularity is 30s; keep recent)
	now := time.to_unix_seconds(time.now())
	kept := [dynamic]UsedCode{}
	for uc in t.db.used[user] {
		if now - uc.used_at <= 90 {
			append(&kept, uc)
		}
	}
	append(&kept, UsedCode{time_window = window, code = strings.clone(code), used_at = now})
	t.db.used[user] = kept[:]
	save_db(t)
	sync.rw_mutex_unlock(&t.mu)
}

verify :: proc(t: ^TOTP, user, code: string) -> Error {
	b, ok := get_binding(t, user)
	if !ok || !b.enabled {
		return .Not_Enabled
	}
	if b.locked_until > 0 && time.to_unix_seconds(time.now()) < b.locked_until {
		return .Account_Locked
	}
	secret, dok := decrypt(t, b)
	if !dok {
		return .Crypto_Failure
	}
	valid, window := verify_totp(secret, code)
	if !valid {
		new_attempts := b.failed_attempts + 1
		locked: i64 = 0
		if new_attempts >= 5 {
			locked = time.to_unix_seconds(time.now()) + 15*60
		}
		update_failed_attempts(t, user, new_attempts, locked)
		return .Invalid_Code
	}
	if is_code_used(t, user, window, code) {
		return .Code_Already_Used
	}
	save_used_code(t, user, window, code)
	update_failed_attempts(t, user, 0, 0)
	return .None
}

verify_backup :: proc(t: ^TOTP, user, code: string) -> Error {
	chash := hash_backup_code(code)
	sync.rw_mutex_lock(&t.mu)
	defer sync.rw_mutex_unlock(&t.mu)
	list := t.db.backups[user]
	for idx in 0..<len(list) {
		if list[idx].code_hash != chash {
			continue
		}
		if list[idx].used {
			return .Invalid_Backup_Code
		}
		list[idx].used = true
		t.db.backups[user] = list
		save_db(t)
		return .None
	}
	return .Invalid_Backup_Code
}

disable :: proc(t: ^TOTP, user: string) -> Error {
	b, ok := get_binding(t, user)
	if !ok {
		return .Not_Setup
	}
	sync.rw_mutex_lock(&t.mu)
	b.enabled = false
	b.updated_at = time.to_unix_seconds(time.now())
	t.db.bindings[user] = b
	ok_save := save_db(t)
	sync.rw_mutex_unlock(&t.mu)
	if !ok_save {
		return .Storage_Failure
	}
	return .None
}

count_unused_backups :: proc(t: ^TOTP, user: string) -> int {
	sync.rw_mutex_shared_lock(&t.mu)
	defer sync.rw_mutex_shared_unlock(&t.mu)
	n := 0
	for bc in t.db.backups[user] {
		if !bc.used {
			n += 1
		}
	}
	return n
}

status :: proc(t: ^TOTP, user: string) -> StatusResult {
	b, ok := get_binding(t, user)
	return StatusResult{
		enabled      = ok && b.enabled,
		backup_codes = count_unused_backups(t, user),
		has_binding  = ok,
	}
}

regenerate_backup_codes :: proc(t: ^TOTP, user: string) -> ([]string, Error) {
	b, ok := get_binding(t, user)
	if !ok || !b.enabled {
		return nil, .Not_Enabled
	}
	codes := generate_backup_codes(10)
	sync.rw_mutex_lock(&t.mu)
	bcs := [dynamic]BackupCode{}
	for c in codes {
		append(&bcs, BackupCode{code_hash = strings.clone(hash_backup_code(c)), used = false})
	}
	t.db.backups[user] = bcs[:]
	ok_save := save_db(t)
	sync.rw_mutex_unlock(&t.mu)
	if !ok_save {
		return nil, .Storage_Failure
	}
	return codes, .None
}
