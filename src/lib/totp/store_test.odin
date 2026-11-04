package totp

// The store file is the one piece of state an operator can also edit by hand,
// so its shape has to keep working across versions: these tests pin the format
// that `encoding/json` used to produce and check that a store with members we
// no longer write still loads.

import "core:mem/virtual"
import "core:strings"
import "core:testing"

@(test)
test_store_round_trip :: proc(t: ^testing.T) {
	arena: virtual.Arena
	testing.expect_value(t, virtual.arena_init_growing(&arena), nil)
	defer virtual.arena_destroy(&arena)
	context.allocator = virtual.arena_allocator(&arena)

	db := empty_db()
	db.bindings["alice"] = Binding {
		user_id         = "alice",
		secret_enc      = "c2VjcmV0",
		enabled         = true,
		failed_attempts = 2,
		locked_until    = 1_700_000_000,
		created_at      = 1_699_000_000,
		updated_at      = 1_699_500_000,
	}
	db.backups["alice"] = []BackupCode {
		{code_hash = "aaaa", used = false},
		{code_hash = "bbbb", used = true},
	}
	db.used["alice"] = []UsedCode{{time_window = 58_000_000, code = "123456", used_at = 1_699_600_000}}

	blob := encode_db(db)
	back, ok := decode_db(blob)
	testing.expect(t, ok, "a store written by encode_db must load")

	got := back.bindings["alice"]
	testing.expect_value(t, got.user_id, "alice")
	testing.expect_value(t, got.secret_enc, "c2VjcmV0")
	testing.expect_value(t, got.enabled, true)
	testing.expect_value(t, got.failed_attempts, 2)
	testing.expect_value(t, got.locked_until, i64(1_700_000_000))
	testing.expect_value(t, got.updated_at, i64(1_699_500_000))

	testing.expect_value(t, len(back.backups["alice"]), 2)
	testing.expect_value(t, back.backups["alice"][1].code_hash, "bbbb")
	testing.expect_value(t, back.backups["alice"][1].used, true)

	testing.expect_value(t, len(back.used["alice"]), 1)
	testing.expect_value(t, back.used["alice"][0].time_window, i64(58_000_000))
	testing.expect_value(t, back.used["alice"][0].code, "123456")
}

@(test)
test_store_reads_the_legacy_shape :: proc(t: ^testing.T) {
	arena: virtual.Arena
	testing.expect_value(t, virtual.arena_init_growing(&arena), nil)
	defer virtual.arena_destroy(&arena)
	context.allocator = virtual.arena_allocator(&arena)

	// Exactly what core:encoding/json emitted: declaration order, no spaces.
	legacy := `{"bindings":{"alice":{"user_id":"alice","secret_enc":"abc","enabled":true,"failed_attempts":0,"locked_until":0,"created_at":1,"updated_at":2}},"backups":{"alice":[{"code_hash":"h1","used":false}]},"used":{"alice":[{"time_window":10,"code":"000000","used_at":20}]}}`
	db, ok := decode_db(transmute([]byte)legacy)
	testing.expect(t, ok, "a store written by the previous build must load")
	testing.expect_value(t, db.bindings["alice"].secret_enc, "abc")
	testing.expect_value(t, db.bindings["alice"].enabled, true)
	testing.expect_value(t, len(db.backups["alice"]), 1)
	testing.expect_value(t, db.used["alice"][0].used_at, i64(20))
}

@(test)
test_store_skips_unknown_members :: proc(t: ^testing.T) {
	arena: virtual.Arena
	testing.expect_value(t, virtual.arena_init_growing(&arena), nil)
	defer virtual.arena_destroy(&arena)
	context.allocator = virtual.arena_allocator(&arena)

	body := `{"version":2,"bindings":{"bob":{"user_id":"bob","secret_enc":"xyz"}},"backups":{},"used":{}}`
	db, ok := decode_db(transmute([]byte)body)
	testing.expect(t, ok, "an unknown member must not fail the load")
	testing.expect_value(t, db.bindings["bob"].secret_enc, "xyz")
	testing.expect_value(t, len(db.backups), 0)
}

@(test)
test_store_rejects_damaged_file :: proc(t: ^testing.T) {
	arena: virtual.Arena
	testing.expect_value(t, virtual.arena_init_growing(&arena), nil)
	defer virtual.arena_destroy(&arena)
	context.allocator = virtual.arena_allocator(&arena)

	damaged := []string{
		``,
		`not json at all`,
		`{"bindings":{"alice":{"user_id":"alice"`,
		`{"bindings":[]}`,
		`{"used":{"alice":[{"time_window":"not a number"}]}}`,
	}
	for body in damaged {
		_, ok := decode_db(transmute([]byte)body)
		testing.expectf(t, !ok, "accepted damaged store {:q}", body)
	}
}

@(test)
test_store_encodes_empty :: proc(t: ^testing.T) {
	arena: virtual.Arena
	testing.expect_value(t, virtual.arena_init_growing(&arena), nil)
	defer virtual.arena_destroy(&arena)
	context.allocator = virtual.arena_allocator(&arena)

	blob := encode_db(empty_db())
	testing.expect_value(t, string(blob), `{"bindings":{},"backups":{},"used":{}}`)
}
