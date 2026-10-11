package log

import "core:os"
import "core:fmt"
import "core:time"
import "core:strings"
import "core:sync"

// Levels mirror slog levels used by the Go version (TextHandler style).
Level :: enum {
	Debug,
	Info,
	Warn,
	Error,
}

mu     : sync.Mutex
level  : Level = .Info
inited : bool

set_level_from_env :: proc() {
	if mode, ok := os.lookup_env_alloc("GORCH_MODE", context.temp_allocator); ok && mode == "dev" {
		level = .Debug
	} else {
		level = .Info
	}
	inited = true
}

// init configures the global logger from the environment. Safe to call repeatedly.
init :: proc() {
	sync.mutex_lock(&mu)
	defer sync.mutex_unlock(&mu)
	if !inited {
		set_level_from_env()
	}
}

// set_level overrides the global minimum level (used by tests).
set_level :: proc(l: Level) {
	sync.mutex_lock(&mu)
	defer sync.mutex_unlock(&mu)
	level = l
	inited = true
}

level_tag :: proc(l: Level) -> string {
	switch l {
	case .Debug: return "DEBUG"
	case .Info:  return "INFO"
	case .Warn:  return "WARN"
	case .Error: return "ERROR"
	}
	return "INFO"
}

log_at :: proc(l: Level, msg: string, kv: ..any) {
	if !inited {
		init()
	}
	sync.mutex_lock(&mu)
	defer sync.mutex_unlock(&mu)
	if l < level {
		return
	}

	now := time.now()
	hb: [16]u8
	hms := time.time_to_string_hms(now, hb[:])
	db: [16]u8
	ymd := time.to_string_yyyy_mm_dd(now, db[:])
	buf: strings.Builder
	strings.builder_init_len(&buf, 256, context.temp_allocator)
	defer strings.builder_destroy(&buf)

	fmt.sbprintf(&buf, "{} {} level={} msg={}", ymd, hms, level_tag(l), msg)
	i := 0
	for i + 1 < len(kv) {
		fmt.sbprintf(&buf, " {}={}", kv[i], kv[i+1])
		i += 2
	}
	strings.write_byte(&buf, '\n')

	os.write_string(os.stderr, strings.to_string(buf))
}

debug :: proc(msg: string, kv: ..any) { log_at(.Debug, msg, ..kv) }
info  :: proc(msg: string, kv: ..any) { log_at(.Info, msg, ..kv) }
warn  :: proc(msg: string, kv: ..any) { log_at(.Warn, msg, ..kv) }
error :: proc(msg: string, kv: ..any) { log_at(.Error, msg, ..kv) }
