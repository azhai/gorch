package jwt

import "core:testing"
import "core:mem"
import "core:fmt"

@(test)
test_jwt_roundtrip :: proc(t: ^testing.T) {
	buf: [1 << 16]byte
	ar: mem.Arena
	mem.arena_init(&ar, buf[:])
	context.allocator = mem.arena_allocator(&ar)

	secret := "s3cr3t-pass"
	tok := generate("admin", secret)
	testing.expect(t, len(tok) > 0)
	claims, ok := validate(tok, secret)
	testing.expect(t, ok)
	testing.expect(t, claims.sub == "admin")

	// wrong secret must fail
	_, ok = validate(tok, "wrong")
	testing.expect(t, !ok)

	// malformed token must fail
	_, ok = validate("not.a.jwt", secret)
	testing.expect(t, !ok)

	// tampered signature must fail (replace trailing segment)
	bad := fmt.tprintf("{}AAA", tok[0 : len(tok)-3])
	_, ok = validate(bad, secret)
	testing.expect(t, !ok)
}
