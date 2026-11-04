package toml

import "core:testing"
import "core:mem/virtual"

@(test)
test_parse_basic :: proc(t: ^testing.T) {
	// The parser allocates freely and never frees, so give it an arena. A growing
	// virtual arena (chained virtual reservations) keeps the 1 MiB working set off
	// the thread stack, which is what the old `buf: [1 << 20]byte` was doing.
	ar: virtual.Arena
	if err := virtual.arena_init_growing(&ar); err != .None {
		testing.fail_now(t, "cannot create the test arena")
	}
	defer virtual.arena_destroy(&ar)

	// Hand the arena to this test only; restore the caller's allocator on the way
	// out so later tests do not allocate from a destroyed arena.
	prev_allocator := context.allocator
	context.allocator = virtual.arena_allocator(&ar)
	defer context.allocator = prev_allocator

	text := `
# comment
LOG_DIR = "/tmp/x"
PID_FILE = '/tmp/y'
COUNT = 42
ENABLED = true
DISABLED = false
LIST = ["a", "b", "c"]

[web]
WEB_ENABLE = true
WEB_ADDR = "127.0.0.1:8080"

[services.foo]
WORK_DIR = "/srv"
EXEC_CMD = "echo hi"
ENV_VARS = {A = "1", B = "2"}
DEPENDS_ON = ["bar"]
`
	root, err := parse_text(text)
	testing.expect(t, err.msg == "")

	s, ok := get_string(root, "LOG_DIR")
	testing.expect(t, ok && s == "/tmp/x")
	s2, _ := get_string(root, "PID_FILE")
	testing.expect(t, s2 == "/tmp/y")
	c, ok2 := get_int(root, "COUNT")
	testing.expect(t, ok2 && c == 42)
	e, _ := get_bool(root, "ENABLED")
	testing.expect(t, e == true)
	d, _ := get_bool(root, "DISABLED")
	testing.expect(t, d == false)
	lst, ok3 := get_array(root, "LIST")
	testing.expect(t, ok3 && len(lst) == 3)
	if len(lst) == 3 {
		testing.expect(t, lst[0].s == "a" && lst[2].s == "c")
	}

	web, ok4 := get_table(root, "web")
	testing.expect(t, ok4)
	we, _ := get_bool(web, "WEB_ENABLE")
	testing.expect(t, we == true)
	wa, _ := get_string(web, "WEB_ADDR")
	testing.expect(t, wa == "127.0.0.1:8080")

	svcs, ok5 := get_table(root, "services")
	testing.expect(t, ok5)
	foo, ok6 := get_table(svcs, "foo")
	testing.expect(t, ok6)
	ec, _ := get_string(foo, "EXEC_CMD")
	testing.expect(t, ec == "echo hi")
	ev, ok7 := get_table(foo, "ENV_VARS")
	testing.expect(t, ok7)
	a, _ := get_string(ev, "A")
	testing.expect(t, a == "1")
	dep, ok8 := get_array(foo, "DEPENDS_ON")
	testing.expect(t, ok8 && len(dep) == 1 && dep[0].s == "bar")
}
