package json

// A small JSON reader and writer.
//
// `core:encoding/json` is reflection based. Linking it costs around 70 KB of
// code and type information - a tenth of a gorch binary - for a handful of
// call sites whose shapes are known at compile time: the config form's request
// bodies and the TOTP store. This package covers exactly what those need and
// nothing else: a forward cursor over a body, and writers that emit the field
// names the caller spells out.
//
// Reading is tolerant in the two ways that matter for an upgrade path: unknown
// object members are skipped, and numbers may carry a fraction or an exponent
// even though every field here is an integer.
//
// `cursor` returns strings as slices of the body it reads: nothing is copied
// until a value contains a backslash. That is the right default for a body
// that is consumed on the spot, and the wrong one for a body that fills a
// structure which outlives it - a request body lives on the connection's
// stack. `owning_cursor` copies every string into the allocator it is given;
// use it for anything persistent.

import "base:runtime"
import "core:fmt"
import "core:strconv"
import "core:strings"

// How deep a body may nest before it is rejected. The deepest real body is a
// config inside a create request; the limit keeps `skip` away from the stack
// guard page on a hostile input.
MAX_DEPTH :: 16

Cursor :: struct {
	src:   string,
	pos:   int,
	depth: int,
	err:   string,
	// Where strings that need allocating come from, and - when `own` is set -
	// where every string comes from.
	allocator: runtime.Allocator,
	// own makes even escape-free strings copies. Set it whenever the values
	// outlive the body they were read from: a request body lives on the
	// connection's stack, and the struct it filled may outlive the request.
	own: bool,
}

// cursor reads a body it will finish with before the body goes away. Strings
// it returns are slices into `src`.
cursor :: proc(src: string) -> Cursor {
	return Cursor{src = src, allocator = context.allocator}
}

// owning_cursor reads a body whose values must survive the buffer. Every
// string is cloned into `allocator`.
owning_cursor :: proc(src: string, allocator: runtime.Allocator) -> Cursor {
	return Cursor{src = src, allocator = allocator, own = true}
}

// ---- errors ----

// fail records the first error and reports false, so a caller can write
// `ok := c->fail(...)` at the point of failure.
fail :: proc(c: ^Cursor, msg: string) -> bool {
	if len(c.err) == 0 {
		c.err = msg
	}
	return false
}

// error_message is the sentence to show a user when parsing stopped.
error_message :: proc(c: ^Cursor) -> string {
	if len(c.err) == 0 {
		return "unexpected end of body"
	}
	return c.err
}

// ---- cursor movement ----

ws :: proc(c: ^Cursor) {
	for c.pos < len(c.src) {
		switch c.src[c.pos] {
		case ' ', '\t', '\r', '\n':
			c.pos += 1
		case:
			return
		}
	}
}

at :: proc(c: ^Cursor, ch: u8) -> bool {
	ws(c)
	return c.pos < len(c.src) && c.src[c.pos] == ch
}

// take consumes `ch` or records an error.
take :: proc(c: ^Cursor, ch: u8) -> bool {
	ws(c)
	if c.pos >= len(c.src) || c.src[c.pos] != ch {
		return fail(c, fmt.tprintf("expected '{}'", rune(ch)))
	}
	c.pos += 1
	return true
}

// ---- values ----

// clone copies a string out of the body into the cursor's allocator.
@(private)
clone :: proc(c: ^Cursor, s: string) -> string {
	return strings.clone(s, c.allocator)
}

// read_string reads a string value. Unless the cursor owns its strings, the
// common case - no escapes - is a slice into the body.
read_string :: proc(c: ^Cursor) -> (string, bool) {
	ws(c)
	if !take(c, '"') {
		return "", false
	}
	start := c.pos
	for i := start; i < len(c.src); i += 1 {
		switch c.src[i] {
		case '"':
			c.pos = i + 1
			if c.own {
				return clone(c, c.src[start:i]), true
			}
			return c.src[start:i], true
		case '\\':
			c.pos = i
			return read_string_escaped(c, start)
		case:
			if c.src[i] < 0x20 {
				return "", fail(c, "control character in string")
			}
		}
	}
	return "", fail(c, "unterminated string")
}

// read_string_escaped finishes a string that contains a backslash. `start` is
// the first byte of the value and c.pos is at the first backslash seen.
@(private)
read_string_escaped :: proc(c: ^Cursor, start: int) -> (string, bool) {
	b := strings.builder_make(c.allocator)
	strings.write_string(&b, c.src[start:c.pos])
	// Start at the backslash itself so the escape branch handles the first
	// escaped character; starting one past it would read `\"` as the end of
	// the string.
	i := c.pos
	for i < len(c.src) {
		ch := c.src[i]
		if ch == '"' {
			c.pos = i + 1
			return strings.to_string(b), true
		}
		if ch != '\\' {
			if ch < 0x20 {
				delete(b.buf)
				return "", fail(c, "control character in string")
			}
			strings.write_byte(&b, ch)
			i += 1
			continue
		}
		if i + 1 >= len(c.src) {
			break
		}
		i += 1
		switch c.src[i] {
		case '"':
			strings.write_byte(&b, '"')
			i += 1
		case '\\':
			strings.write_byte(&b, '\\')
			i += 1
		case '/':
			strings.write_byte(&b, '/')
			i += 1
		case 'b':
			strings.write_byte(&b, '\b')
			i += 1
		case 'f':
			strings.write_byte(&b, '\f')
			i += 1
		case 'n':
			strings.write_byte(&b, '\n')
			i += 1
		case 'r':
			strings.write_byte(&b, '\r')
			i += 1
		case 't':
			strings.write_byte(&b, '\t')
			i += 1
		case 'u':
			code, next, ok := hex4(c, i + 1)
			if !ok {
				delete(b.buf)
				return "", false
			}
			i = next
			// A character outside the BMP arrives as a surrogate pair.
			if code >= 0xD800 && code < 0xDC00 && i + 1 < len(c.src) &&
			   c.src[i] == '\\' && c.src[i+1] == 'u' {
				if low, next2, ok2 := hex4(c, i + 2); ok2 && low >= 0xDC00 && low < 0xE000 {
					code = 0x10000 + (code - 0xD800) * 0x400 + (low - 0xDC00)
					i = next2
				}
			}
			if _, rune_err := strings.write_rune(&b, rune(code)); rune_err != .None {
				delete(b.buf)
				return "", fail(c, "invalid code point")
			}
		case:
			delete(b.buf)
			return "", fail(c, "invalid escape")
		}
	}
	delete(b.buf)
	return "", fail(c, "unterminated string")
}

@(private)
hex4 :: proc(c: ^Cursor, i: int) -> (value: u32, next: int, ok: bool) {
	for k in 0 ..< 4 {
		if i + k >= len(c.src) {
			return 0, i, fail(c, "truncated \\u escape")
		}
		d := c.src[i+k]
		switch {
		case d >= '0' && d <= '9':
			value = value * 16 + u32(d - '0')
		case d >= 'a' && d <= 'f':
			value = value * 16 + u32(d - 'a' + 10)
		case d >= 'A' && d <= 'F':
			value = value * 16 + u32(d - 'A' + 10)
		case:
			return 0, i, fail(c, "invalid \\u escape")
		}
	}
	return value, i + 4, true
}

// read_int reads an integer. A fraction or exponent is accepted and dropped:
// every field gorch persists is a whole number, but a body written by another
// client may still say 1.0.
read_int :: proc(c: ^Cursor) -> (int, bool) {
	value, ok := read_i64(c)
	return int(value), ok
}

read_i64 :: proc(c: ^Cursor) -> (i64, bool) {
	ws(c)
	negative := false
	if c.pos < len(c.src) && c.src[c.pos] == '-' {
		negative = true
		c.pos += 1
	}
	start := c.pos
	for c.pos < len(c.src) && c.src[c.pos] >= '0' && c.src[c.pos] <= '9' {
		c.pos += 1
	}
	if start == c.pos {
		return 0, fail(c, "expected a number")
	}
	value, ok := strconv.parse_i64(c.src[start:c.pos])
	if !ok {
		return 0, fail(c, "number out of range")
	}
	skip_fraction(c)
	return negative ? -value : value, true
}

@(private)
skip_fraction :: proc(c: ^Cursor) {
	if c.pos >= len(c.src) {
		return
	}
	switch c.src[c.pos] {
	case '.', 'e', 'E':
	case:
		return
	}
	for c.pos < len(c.src) {
		ch := c.src[c.pos]
		if (ch >= '0' && ch <= '9') || ch == '.' || ch == 'e' || ch == 'E' || ch == '+' || ch == '-' {
			c.pos += 1
			continue
		}
		return
	}
}

read_bool :: proc(c: ^Cursor) -> (bool, bool) {
	ws(c)
	rest := c.src[c.pos:]
	switch {
	case strings.has_prefix(rest, "true"):
		c.pos += 4
		return true, true
	case strings.has_prefix(rest, "false"):
		c.pos += 5
		return false, true
	}
	return false, fail(c, "expected true or false")
}

// ---- containers ----

// object_begin consumes the '{' that opens an object.
object_begin :: proc(c: ^Cursor) -> bool {
	if c.depth >= MAX_DEPTH {
		return fail(c, "body nests too deeply")
	}
	c.depth += 1
	return take(c, '{')
}

// object_end consumes the '}' that closes an object. It is not needed after a
// loop that ran until `key` returned false, which already consumed it.
object_end :: proc(c: ^Cursor) -> bool {
	c.depth -= 1
	return take(c, '}')
}

// key advances inside an object: it consumes the separator, reads the member
// name and the colon. It returns false at the closing brace, which is how the
// caller's loop ends, and also on error - tell them apart with c.err.
key :: proc(c: ^Cursor, first: ^bool) -> (string, bool) {
	ws(c)
	if c.pos >= len(c.src) {
		return "", fail(c, "unexpected end of object")
	}
	if c.src[c.pos] == '}' {
		c.pos += 1
		c.depth -= 1
		return "", false
	}
	if !first^ {
		if !take(c, ',') {
			return "", false
		}
		ws(c)
	}
	first^ = false
	name, ok := read_string(c)
	if !ok {
		return "", false
	}
	if !take(c, ':') {
		return "", false
	}
	return name, true
}

// array_begin consumes the '[' that opens an array. It reports false, with no
// error set, for an empty array so the caller can stop right away.
array_begin :: proc(c: ^Cursor) -> (empty: bool, ok: bool) {
	if c.depth >= MAX_DEPTH {
		return false, fail(c, "body nests too deeply")
	}
	c.depth += 1
	if !take(c, '[') {
		return false, false
	}
	if at(c, ']') {
		c.pos += 1
		c.depth -= 1
		return true, true
	}
	return false, true
}

// array_next consumes the ',' between elements and reports false at the ']'
// that closes the array.
array_next :: proc(c: ^Cursor) -> bool {
	ws(c)
	if c.pos < len(c.src) && c.src[c.pos] == ',' {
		c.pos += 1
		return true
	}
	if c.pos < len(c.src) && c.src[c.pos] == ']' {
		c.pos += 1
		c.depth -= 1
		return false
	}
	return fail(c, "expected ',' or ']'")
}

read_string_array :: proc(c: ^Cursor) -> ([]string, bool) {
	empty, ok := array_begin(c)
	if !ok {
		return nil, false
	}
	out := make([dynamic]string, 0, 4, c.allocator)
	if empty {
		return out[:], true
	}
	for {
		value, value_ok := read_string(c)
		if !value_ok {
			delete(out)
			return nil, false
		}
		append(&out, value)
		if !array_next(c) {
			break
		}
	}
	if len(c.err) > 0 {
		delete(out)
		return nil, false
	}
	return out[:], true
}

read_string_map :: proc(c: ^Cursor) -> (map[string]string, bool) {
	if !object_begin(c) {
		return nil, false
	}
	out := make(map[string]string, 4, c.allocator)
	first := true
	for {
		name, more := key(c, &first)
		if !more {
			break
		}
		value, value_ok := read_string(c)
		if !value_ok {
			delete(out)
			return nil, false
		}
		out[name] = value
	}
	if len(c.err) > 0 {
		delete(out)
		return nil, false
	}
	return out, true
}

// skip steps over a value the caller does not read.
skip :: proc(c: ^Cursor) -> bool {
	ws(c)
	if c.pos >= len(c.src) {
		return fail(c, "unexpected end of body")
	}
	switch c.src[c.pos] {
	case '"':
		_, ok := read_string(c)
		return ok
	case '{':
		if !object_begin(c) {
			return false
		}
		first := true
		for {
			_, more := key(c, &first)
			if !more {
				break
			}
			if !skip(c) {
				return false
			}
		}
		return len(c.err) == 0
	case '[':
		empty, ok := array_begin(c)
		if !ok || empty {
			return ok
		}
		for {
			if !skip(c) {
				return false
			}
			if !array_next(c) {
				break
			}
		}
		return len(c.err) == 0
	case:
		// true / false / null / a number: a run of literal characters.
		start := c.pos
		for c.pos < len(c.src) {
			ch := c.src[c.pos]
			if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') ||
			   ch == '+' || ch == '-' || ch == '.' {
				c.pos += 1
				continue
			}
			break
		}
		if start == c.pos {
			return fail(c, "expected a value")
		}
		return true
	}
}

// ---- writing ----

// write_string quotes and escapes a string. Control characters below 0x20 are
// not representable raw in JSON; log lines carry ANSI escapes, and some of
// those are in that range.
write_string :: proc(b: ^strings.Builder, s: string) {
	strings.write_byte(b, '"')
	for i in 0 ..< len(s) {
		switch s[i] {
		case '"':
			strings.write_string(b, "\\\"")
		case '\\':
			strings.write_string(b, "\\\\")
		case '\n':
			strings.write_string(b, "\\n")
		case '\r':
			strings.write_string(b, "\\r")
		case '\t':
			strings.write_string(b, "\\t")
		case:
			if s[i] < 0x20 {
				fmt.sbprintf(b, "\\u{:04x}", u32(s[i]))
			} else {
				strings.write_byte(b, s[i])
			}
		}
	}
	strings.write_byte(b, '"')
}

write_int :: proc(b: ^strings.Builder, n: int) {
	fmt.sbprintf(b, "{}", n)
}

write_i64 :: proc(b: ^strings.Builder, n: i64) {
	fmt.sbprintf(b, "{}", n)
}

write_bool :: proc(b: ^strings.Builder, v: bool) {
	strings.write_string(b, v ? "true" : "false")
}
