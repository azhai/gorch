package state

import "core:mem"
import "base:runtime"
import "core:sync"
import "core:time"
import "oboe:cache"

// gorch keeps its runtime state (which service runs under which pid, exit codes,
// restart counts) in an oboe cache: an in-memory key/value store that snapshots
// itself to a single compressed file through `<path>.tmp` + rename. That replaces
// both SQLite and pid files — one small artifact, atomically replaced, no schema.
//
// The cache has no key iteration, which is fine: the authoritative list of
// services is `gorch.toml`, so callers look state up by name instead of scanning.

// VERSION is the on-disk record layout. Bump it whenever Record changes; a
// mismatch is treated as "no state" rather than decoding garbage.
VERSION :: 1

// Record is the persisted state of one supervised service. The service name is
// the cache key, so it is not repeated in the value.
Record :: struct {
	status:     u8, // status.Status ordinal
	pid:        int,
	started_at: i64, // unix seconds; 0 when never started
	exited_at:  i64, // unix seconds; 0 while running
	exit_code:  int,
	restarts:   int, // restarts inside the current budget window
	pid_valid:  bool, // false once we know the pid is gone or was reused
}

@(private = "file")
Disk_Record :: struct {
	version:    u8,
	flags:      u8,
	status:     u8,
	_pad0:      u8,
	pid:        i32,
	exit_code:  i32,
	restarts:   i32,
	_pad1:      i32,
	started_at: i64,
	exited_at:  i64,
}

@(private = "file")
FLAG_PID_VALID :: 1 << 0

// encode copies a Record into its fixed-size disk form. The layout is packed and
// little-endian by virtue of being this host's own bytes; a state file is local.
@(private = "file")
encode :: proc(r: Record) -> [size_of(Disk_Record)]u8 {
	dr := Disk_Record {
		version    = VERSION,
		flags      = r.pid_valid ? FLAG_PID_VALID : 0,
		status     = r.status,
		pid        = i32(r.pid),
		exit_code  = i32(r.exit_code),
		restarts   = i32(r.restarts),
		started_at = r.started_at,
		exited_at  = r.exited_at,
	}
	out: [size_of(Disk_Record)]u8
	copy(out[:], mem.byte_slice(&dr, size_of(Disk_Record)))
	return out
}

@(private = "file")
decode :: proc(raw: []u8) -> (Record, bool) {
	if len(raw) != size_of(Disk_Record) {
		return {}, false
	}
	dr: Disk_Record
	copy(mem.byte_slice(&dr, size_of(Disk_Record)), raw)
	if dr.version != VERSION {
		return {}, false
	}
	return Record {
			status = dr.status,
			pid = int(dr.pid),
			started_at = dr.started_at,
			exited_at = dr.exited_at,
			exit_code = int(dr.exit_code),
			restarts = int(dr.restarts),
			pid_valid = (dr.flags & FLAG_PID_VALID) != 0,
		},
		true
}

// Store wraps the cache plus the file it snapshots to.
Store :: struct {
	path:      string,
	cfg:       cache.Cache_Config,
	c:         ^cache.Cache,
	last_save: time.Time,
	has_saved: bool,
	dirty:     bool,
	save_mu:   sync.Mutex,
}

// open loads the state file when it exists and decodes cleanly, otherwise starts
// empty. `loaded` says which happened; it is not an error signal.
open :: proc(path: string) -> (s: Store, loaded: bool) {
	cfg := cache.default_cache_config()
	cfg.max_bytes = 4 << 20
	cfg.max_entries = 4096
	cfg.max_value = 64 << 10

	// oboe pins the bucket maps to whichever allocator was ambient when they were
	// created and frees them with the ambient one, so init/load (and destroy in
	// close) must run on the default allocator — never inside an arena scope.
	prev := context.allocator
	context.allocator = runtime.default_allocator()
	defer context.allocator = prev

	s = Store{path = path, cfg = cfg}
	if c, ok := cache.cache_load_file(cfg, path); ok {
		s.c = c
		return s, true
	}
	s.c = cache.cache_init(cfg)
	return s, false
}

close :: proc(s: ^Store) {
	if s.c == nil {
		return
	}
	prev := context.allocator
	context.allocator = runtime.default_allocator()
	cache.cache_destroy(s.c)
	context.allocator = prev
	s.c = nil
}

// MAX_KEY is the longest key this package will build; cache keys are service
// names, which are short by construction.
MAX_KEY :: 96

// key_of builds "<prefix><name>" into buf without allocating. The returned string
// aliases buf, so it must not outlive the caller's stack frame.
@(private = "file")
key_of :: proc(buf: []u8, prefix, name: string) -> (string, bool) {
	total := len(prefix) + len(name)
	if total > len(buf) || total == 0 {
		return "", false
	}
	n := copy(buf, prefix)
	n += copy(buf[n:], name)
	return transmute(string)buf[:n], true
}

SERVICE_PREFIX :: "svc:"

// save_service writes one service record. ttl is 0: state never expires on its
// own, only the supervisor's pid liveness check invalidates it.
save_service :: proc(s: ^Store, name: string, r: Record) -> bool {
	buf: [MAX_KEY]u8
	key, ok := key_of(buf[:], SERVICE_PREFIX, name)
	if !ok {
		return false
	}
	blob := encode(r)
	written := cache.cache_set(s.c, key, blob[:], 0)
	if written {
		s.dirty = true
	}
	return written
}

// load_service returns the stored record. The buffer is allocated with the
// caller's allocator and must be freed by this package (blob_get frees it).
load_service :: proc(s: ^Store, name: string) -> (Record, bool) {
	buf: [MAX_KEY]u8
	key, ok := key_of(buf[:], SERVICE_PREFIX, name)
	if !ok {
		return {}, false
	}
	raw, found := cache.cache_get(s.c, key)
	if !found {
		return {}, false
	}
	defer delete(raw)
	return decode(raw)
}

// put_blob stores an arbitrary small value (meta keys, TOTP secrets later). The
// value is copied by the cache.
put_blob :: proc(s: ^Store, key: string, value: []u8) -> bool {
	written := cache.cache_set(s.c, key, value, 0)
	if written {
		s.dirty = true
	}
	return written
}

// get_blob returns a copy of the value, allocated with the caller's allocator;
// the caller owns and must delete it.
get_blob :: proc(s: ^Store, key: string) -> ([]u8, bool) {
	return cache.cache_get(s.c, key)
}

remove_blob :: proc(s: ^Store, key: string) -> bool {
	removed := cache.cache_delete(s.c, key)
	if removed {
		s.dirty = true
	}
	return removed
}

// save rewrites the snapshot file. oboe serializes concurrent saves internally
// and replaces the file atomically, so this is safe to call from any thread.
save :: proc(s: ^Store) -> bool {
	sync.mutex_lock(&s.save_mu)
	defer sync.mutex_unlock(&s.save_mu)
	if _, ok := cache.cache_save_file(s.c, s.path); !ok {
		return false
	}
	s.last_save = time.now()
	s.has_saved = true
	s.dirty = false
	return true
}

// save_if_due snapshots when something changed and `every` has elapsed. It makes
// the periodic writer cheap: no writes when nothing moved.
save_if_due :: proc(s: ^Store, every: time.Duration) -> bool {
	if !s.dirty {
		return false
	}
	if s.has_saved && time.since(s.last_save) < every {
		return false
	}
	return save(s)
}
