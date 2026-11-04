package totp

// The on-disk form of the TOTP store.
//
// The file is JSON because an operator may need to look at it, and because the
// Go service this daemon replaced used the same shape:
//
//	{"bindings":{"alice":{"user_id":"alice","secret_enc":"...",...}},
//	 "backups":{"alice":[{"code_hash":"...","used":false}]},
//	 "used":{"alice":[{"time_window":123,"code":"...","used_at":456}]}}
//
// It is read and written by hand rather than through `core:encoding/json`,
// which costs about 70 KB of reflection code for this one file. Reading skips
// members it does not know, so a file written by a newer build still loads.

import "core:strings"

import "../json"

// decode_db parses a store file. A member that is not an object or an array of
// the expected shape fails the whole file: a half-read store would silently
// drop a user's second factor.
decode_db :: proc(data: []byte) -> (db: DB, ok: bool) {
	// The store outlives the file it was read from, so every string has to be
	// copied out of the buffer.
	c := json.owning_cursor(string(data), context.allocator)
	db = empty_db()
	if !json.object_begin(&c) {
		return empty_db(), false
	}
	first := true
	for {
		name, more := json.key(&c, &first)
		if !more {
			break
		}
		member_ok: bool
		switch name {
		case "bindings":
			member_ok = decode_bindings(&c, &db)
		case "backups":
			member_ok = decode_backups(&c, &db)
		case "used":
			member_ok = decode_used(&c, &db)
		case:
			member_ok = json.skip(&c)
		}
		if !member_ok {
			return empty_db(), false
		}
	}
	return db, len(c.err) == 0
}

@(private)
decode_bindings :: proc(c: ^json.Cursor, db: ^DB) -> bool {
	if !json.object_begin(c) {
		return false
	}
	first := true
	for {
		user, more := json.key(c, &first)
		if !more {
			break
		}
		binding: Binding
		if !json.object_begin(c) {
			return false
		}
		inner := true
		for {
			field, field_more := json.key(c, &inner)
			if !field_more {
				break
			}
			ok := true
			switch field {
			case "user_id":
				binding.user_id, ok = json.read_string(c)
			case "secret_enc":
				binding.secret_enc, ok = json.read_string(c)
			case "enabled":
				binding.enabled, ok = json.read_bool(c)
			case "failed_attempts":
				binding.failed_attempts, ok = json.read_int(c)
			case "locked_until":
				binding.locked_until, ok = json.read_i64(c)
			case "created_at":
				binding.created_at, ok = json.read_i64(c)
			case "updated_at":
				binding.updated_at, ok = json.read_i64(c)
			case:
				ok = json.skip(c)
			}
			if !ok {
				return false
			}
		}
		db.bindings[user] = binding
	}
	return len(c.err) == 0
}

@(private)
decode_backups :: proc(c: ^json.Cursor, db: ^DB) -> bool {
	if !json.object_begin(c) {
		return false
	}
	first := true
	for {
		user, more := json.key(c, &first)
		if !more {
			break
		}
		codes := make([dynamic]BackupCode, 0, 10)
		if !decode_backup_codes(c, &codes) {
			delete(codes)
			return false
		}
		db.backups[user] = codes[:]
	}
	return len(c.err) == 0
}

@(private)
decode_backup_codes :: proc(c: ^json.Cursor, codes: ^[dynamic]BackupCode) -> bool {
	empty, ok := json.array_begin(c)
	if !ok {
		return false
	}
	if empty {
		return true
	}
	for {
		code: BackupCode
		if !json.object_begin(c) {
			return false
		}
		inner := true
		for {
			field, field_more := json.key(c, &inner)
			if !field_more {
				break
			}
			field_ok := true
			switch field {
			case "code_hash":
				code.code_hash, field_ok = json.read_string(c)
			case "used":
				code.used, field_ok = json.read_bool(c)
			case:
				field_ok = json.skip(c)
			}
			if !field_ok {
				return false
			}
		}
		append(codes, code)
		if !json.array_next(c) {
			break
		}
	}
	return len(c.err) == 0
}

@(private)
decode_used :: proc(c: ^json.Cursor, db: ^DB) -> bool {
	if !json.object_begin(c) {
		return false
	}
	first := true
	for {
		user, more := json.key(c, &first)
		if !more {
			break
		}
		entries := make([dynamic]UsedCode, 0, 8)
		if !decode_used_codes(c, &entries) {
			delete(entries)
			return false
		}
		db.used[user] = entries[:]
	}
	return len(c.err) == 0
}

@(private)
decode_used_codes :: proc(c: ^json.Cursor, entries: ^[dynamic]UsedCode) -> bool {
	empty, ok := json.array_begin(c)
	if !ok {
		return false
	}
	if empty {
		return true
	}
	for {
		entry: UsedCode
		if !json.object_begin(c) {
			return false
		}
		inner := true
		for {
			field, field_more := json.key(c, &inner)
			if !field_more {
				break
			}
			field_ok := true
			switch field {
			case "time_window":
				entry.time_window, field_ok = json.read_i64(c)
			case "code":
				entry.code, field_ok = json.read_string(c)
			case "used_at":
				entry.used_at, field_ok = json.read_i64(c)
			case:
				field_ok = json.skip(c)
			}
			if !field_ok {
				return false
			}
		}
		append(entries, entry)
		if !json.array_next(c) {
			break
		}
	}
	return len(c.err) == 0
}

// encode_db renders the store. It is allocated with the calling context's
// allocator and belongs to the caller.
encode_db :: proc(db: DB) -> []u8 {
	b := strings.builder_make()
	strings.write_string(&b, `{"bindings":{`)
	first := true
	for user, binding in db.bindings {
		if !first {
			strings.write_byte(&b, ',')
		}
		first = false
		json.write_string(&b, user)
		strings.write_string(&b, `:{"user_id":`)
		json.write_string(&b, binding.user_id)
		strings.write_string(&b, `,"secret_enc":`)
		json.write_string(&b, binding.secret_enc)
		strings.write_string(&b, `,"enabled":`)
		json.write_bool(&b, binding.enabled)
		strings.write_string(&b, `,"failed_attempts":`)
		json.write_int(&b, binding.failed_attempts)
		strings.write_string(&b, `,"locked_until":`)
		json.write_i64(&b, binding.locked_until)
		strings.write_string(&b, `,"created_at":`)
		json.write_i64(&b, binding.created_at)
		strings.write_string(&b, `,"updated_at":`)
		json.write_i64(&b, binding.updated_at)
		strings.write_byte(&b, '}')
	}
	strings.write_string(&b, `},"backups":{`)
	first = true
	for user, codes in db.backups {
		if !first {
			strings.write_byte(&b, ',')
		}
		first = false
		json.write_string(&b, user)
		strings.write_string(&b, `:[`)
		for code, i in codes {
			if i > 0 {
				strings.write_byte(&b, ',')
			}
			strings.write_string(&b, `{"code_hash":`)
			json.write_string(&b, code.code_hash)
			strings.write_string(&b, `,"used":`)
			json.write_bool(&b, code.used)
			strings.write_byte(&b, '}')
		}
		strings.write_byte(&b, ']')
	}
	strings.write_string(&b, `},"used":{`)
	first = true
	for user, entries in db.used {
		if !first {
			strings.write_byte(&b, ',')
		}
		first = false
		json.write_string(&b, user)
		strings.write_string(&b, `:[`)
		for entry, i in entries {
			if i > 0 {
				strings.write_byte(&b, ',')
			}
			strings.write_string(&b, `{"time_window":`)
			json.write_i64(&b, entry.time_window)
			strings.write_string(&b, `,"code":`)
			json.write_string(&b, entry.code)
			strings.write_string(&b, `,"used_at":`)
			json.write_i64(&b, entry.used_at)
			strings.write_byte(&b, '}')
		}
		strings.write_byte(&b, ']')
	}
	strings.write_string(&b, `}}`)
	return transmute([]u8)strings.to_string(b)
}
