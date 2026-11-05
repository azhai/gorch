// Package glog is gorch's own logger.
//
// It cannot be called `log`: importing config drags in core:log transitively,
// and Odin rejects two packages with the same name in one program.
package glog

import "base:runtime"
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

// line is the one buffer every record is formatted into. The logger already
// serializes on `mu`, so a shared builder is safe, and it removes the last
// reason the logger had to allocate at all.
//
// It used to take 256 bytes per call from context.temp_allocator. That arena
// is thread_local and append-only - nothing ever calls free_all on it - so
// each thread that logged grew its own arena by 256 bytes per line, in 4 MiB
// blocks, for the life of the process. On a router left running for weeks
// that is exactly the kind of resident memory that never comes back.
line: strings.Builder

// line_buffer makes sure `line` exists. It is pinned to the raw heap on
// purpose: the buffer lives as long as the process, and going through
// context.allocator would make it look like a leak to a test's tracking
// allocator.
line_buffer :: proc() -> ^strings.Builder {
	if line.buf == nil {
		strings.builder_init_len_cap(&line, 0, 256, runtime.heap_allocator())
	}
	return &line
}

set_level_from_env :: proc() {
	// Reading the mode allocates; do it on the ordinary heap and hand it back,
	// rather than touching the temp arena before the process has done any work.
	level = .Info
	if mode, ok := os.lookup_env("GORCH_MODE", context.allocator); ok {
		if mode == "dev" {
			level = .Debug
		}
		delete(mode, context.allocator)
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
	// Reset, keep the capacity: the buffer grows to the longest record this
	// process ever writes and then stops allocating. See `line` above.
	buf := line_buffer()
	strings.builder_reset(buf)

	fmt.sbprintf(buf, "{} {} level={} msg={}", ymd, hms, level_tag(l), msg)
	i := 0
	for i + 1 < len(kv) {
		fmt.sbprintf(buf, " {}={}", kv[i], kv[i+1])
		i += 2
	}
	strings.write_byte(buf, '\n')

	os.write_string(os.stderr, strings.to_string(buf^))
}

debug :: proc(msg: string, kv: ..any) { log_at(.Debug, msg, ..kv) }
info  :: proc(msg: string, kv: ..any) { log_at(.Info, msg, ..kv) }
warn  :: proc(msg: string, kv: ..any) { log_at(.Warn, msg, ..kv) }
error :: proc(msg: string, kv: ..any) { log_at(.Error, msg, ..kv) }
