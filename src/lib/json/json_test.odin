#+feature dynamic-literals
package json

// The reader is small enough to look correct and wrong in the same breath:
// every case below is a body the config form or the TOTP store can produce.
// Round-tripping through the writer is checked too, because the two halves
// share the escape table.

import "core:mem/virtual"
import "core:strings"
import "core:testing"

@(test)
test_read_plain_object :: proc(t: ^testing.T) {
	c := cursor(`{"a":"x","b":12,"c":true}`)
	testing.expect(t, object_begin(&c))

	first := true
	seen := 0
	for {
		name, more := key(&c, &first)
		if !more {
			break
		}
		switch name {
		case "a":
			value, ok := read_string(&c)
			testing.expect(t, ok)
			testing.expect_value(t, value, "x")
		case "b":
			value, ok := read_int(&c)
			testing.expect(t, ok)
			testing.expect_value(t, value, 12)
		case "c":
			value, ok := read_bool(&c)
			testing.expect(t, ok)
			testing.expect(t, value)
		case:
			testing.expect(t, skip(&c), "unknown member")
		}
		seen += 1
	}
	testing.expect_value(t, len(c.err), 0)
	testing.expect_value(t, seen, 3)
}

@(test)
test_read_escapes :: proc(t: ^testing.T) {
	arena: virtual.Arena
	testing.expect_value(t, virtual.arena_init_growing(&arena), nil)
	defer virtual.arena_destroy(&arena)
	context.allocator = virtual.arena_allocator(&arena)

	body := `{"a":"echo \"hi\"","b":"line\nbreak","c":"caf\u00e9","d":"tab\there","e":"sla\/sh","f":"back\\slash"}`
	c := cursor(body)
	testing.expect(t, object_begin(&c))
	want := map[string]string {
		"a" = `echo "hi"`,
		"b" = "line\nbreak",
		"c" = "café",
		"d" = "tab\there",
		"e" = "sla/sh",
		"f" = `back\slash`,
	}
	first := true
	for {
		name, more := key(&c, &first)
		if !more {
			break
		}
		value, ok := read_string(&c)
		testing.expectf(t, ok, "reading {}", name)
		testing.expectf(t, value == want[name], "{}: got {:q} want {:q}", name, value, want[name])
	}
	testing.expect_value(t, len(c.err), 0)
}

@(test)
test_read_numbers :: proc(t: ^testing.T) {
	c := cursor(`[-5, 0, 42, 1.5, 2e3]`)
	empty, ok := array_begin(&c)
	testing.expect(t, ok)
	testing.expect(t, !empty)
	want := []int{-5, 0, 42, 1, 2}
	for expected in want {
		value, read_ok := read_int(&c)
		testing.expect(t, read_ok)
		testing.expect_value(t, value, expected)
		array_next(&c)
	}
	testing.expect_value(t, len(c.err), 0)
}

@(test)
test_read_collections :: proc(t: ^testing.T) {
	c := cursor(`{"list":["a","b"],"map":{"k":"v"}}`)
	testing.expect(t, object_begin(&c))
	first := true
	for {
		name, more := key(&c, &first)
		if !more {
			break
		}
		switch name {
		case "list":
			list, ok := read_string_array(&c)
			testing.expect(t, ok)
			testing.expect_value(t, len(list), 2)
			testing.expect_value(t, list[1], "b")
			delete(list)
		case "map":
			m, ok := read_string_map(&c)
			testing.expect(t, ok)
			testing.expect_value(t, m["k"], "v")
			delete(m)
		case:
			testing.expect(t, skip(&c))
		}
	}
	testing.expect_value(t, len(c.err), 0)
}

@(test)
test_skip_nested :: proc(t: ^testing.T) {
	c := cursor(`{"junk":{"a":[1,2,{"b":"c"}],"d":null},"keep":"yes"}`)
	testing.expect(t, object_begin(&c))
	first := true
	kept := ""
	for {
		name, more := key(&c, &first)
		if !more {
			break
		}
		if name == "keep" {
			kept, _ = read_string(&c)
		} else {
			testing.expect(t, skip(&c), "skipping junk")
		}
	}
	testing.expect_value(t, kept, "yes")
	testing.expect_value(t, len(c.err), 0)
}

@(test)
test_rejects_malformed :: proc(t: ^testing.T) {
	arena: virtual.Arena
	testing.expect_value(t, virtual.arena_init_growing(&arena), nil)
	defer virtual.arena_destroy(&arena)
	context.allocator = virtual.arena_allocator(&arena)

	bad := []string{
		`{"a":}`,
		`{"a" "b"}`,
		`{"a":"unterminated}`,
		`{"a":"bad\qescape"}`,
		`{"a":1,}`,
		`{a:1}`,
	}
	for body in bad {
		c := cursor(body)
		ok := object_begin(&c)
		if ok {
			first := true
			for {
				_, more := key(&c, &first)
				if !more {
					break
				}
				if !skip(&c) {
					break
				}
			}
		}
		testing.expectf(t, len(c.err) > 0, "accepted invalid body {}", body)
	}
}

@(test)
test_write_round_trip :: proc(t: ^testing.T) {
	arena: virtual.Arena
	testing.expect_value(t, virtual.arena_init_growing(&arena), nil)
	defer virtual.arena_destroy(&arena)
	context.allocator = virtual.arena_allocator(&arena)

	b := strings.builder_make()
	defer strings.builder_destroy(&b)
	write_string(&b, "a\"b\\c\nd\te\x01")
	out := strings.to_string(b)
	testing.expect_value(t, out, `"a\"b\\c\nd\te\u0001"`)

	c := cursor(out)
	value, ok := read_string(&c)
	testing.expect(t, ok)
	testing.expect_value(t, value, "a\"b\\c\nd\te\x01")
}
