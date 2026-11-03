package logfile

import "core:fmt"
import "core:os"
import "core:strings"
import "core:testing"
import "core:time"

// Each case gets its own directory: odin runs tests on parallel threads, so a
// shared path would let one case truncate another's log.

@(private = "file")
scratch :: proc(t: ^testing.T, tag: string) -> string {
	d, err := os.make_directory_temp("", fmt.tprintf("gorch-log-{}", tag), context.allocator)
	if err != nil {
		testing.fail_now(t, fmt.tprintf("cannot create temp dir: {}", err))
	}
	return d
}

@(private = "file")
write_file :: proc(t: ^testing.T, path, content: string) {
	if err := os.write_entire_file(path, content); err != nil {
		testing.fail_now(t, fmt.tprintf("cannot write {}: {}", path, err))
	}
}

@(test)
test_tail_returns_last_lines :: proc(t: ^testing.T) {
	dir := scratch(t, "tail")
	defer {
		_ = os.remove_all(dir)
		delete(dir)
	}
	path := fmt.tprintf("{}/app.log", dir)

	b := strings.builder_make()
	defer strings.builder_destroy(&b)
	for i in 1 ..= 20 {
		fmt.sbprintf(&b, "line {}\n", i)
	}
	write_file(t, path, strings.to_string(b))

	got, err := tail(path, 5)
	defer tail_destroy(&got)
	testing.expectf(t, err == "", "tail failed: {}", err)
	testing.expectf(t, len(got.lines) == 5, "got {} lines", len(got.lines))
	if len(got.lines) == 5 {
		testing.expect_value(t, got.lines[0], "line 16")
		testing.expect_value(t, got.lines[4], "line 20")
	}
}

@(test)
test_tail_caps_at_available :: proc(t: ^testing.T) {
	dir := scratch(t, "small")
	defer {
		_ = os.remove_all(dir)
		delete(dir)
	}
	path := fmt.tprintf("{}/app.log", dir)
	write_file(t, path, "one\ntwo\n")

	got, err := tail(path, 500)
	defer tail_destroy(&got)
	testing.expectf(t, err == "", "tail failed: {}", err)
	testing.expectf(t, len(got.lines) == 2, "got {} lines", len(got.lines))
	if len(got.lines) == 2 {
		testing.expect_value(t, got.lines[1], "two")
	}
}

// A window that starts mid-file must not emit the partial first line; this is
// the case that a naive read-whole-file implementation never exercises.
@(test)
test_tail_window_starts_mid_line :: proc(t: ^testing.T) {
	dir := scratch(t, "window")
	defer {
		_ = os.remove_all(dir)
		delete(dir)
	}
	path := fmt.tprintf("{}/big.log", dir)

	b := strings.builder_make()
	defer strings.builder_destroy(&b)
	// Wide enough to exceed the first 32K window several times over.
	padding := strings.repeat("x", 200, context.temp_allocator)
	for i in 1 ..= 1000 {
		fmt.sbprintf(&b, "{} line {}\n", padding, i)
	}
	write_file(t, path, strings.to_string(b))

	got, err := tail(path, 10)
	defer tail_destroy(&got)
	testing.expectf(t, err == "", "tail failed: {}", err)
	testing.expectf(t, len(got.lines) == 10, "got {} lines", len(got.lines))
	if len(got.lines) == 10 {
		testing.expect(t, strings.has_prefix(got.lines[0], padding), "first line must be whole")
		testing.expect(t, strings.has_suffix(got.lines[9], " line 1000"), "last line must be the newest")
	}
}

@(test)
test_tail_missing_file :: proc(t: ^testing.T) {
	got, err := tail("/nonexistent/gorch/log.log", 10)
	defer tail_destroy(&got)
	testing.expectf(t, err == "", "a missing log is not an error, got {}", err)
	testing.expect(t, len(got.lines) == 0, "no lines")
}

@(test)
test_tail_strips_crlf :: proc(t: ^testing.T) {
	dir := scratch(t, "crlf")
	defer {
		_ = os.remove_all(dir)
		delete(dir)
	}
	path := fmt.tprintf("{}/win.log", dir)
	write_file(t, path, "alpha\r\nbeta\r\n")

	got, err := tail(path, 10)
	defer tail_destroy(&got)
	testing.expectf(t, err == "", "tail failed: {}", err)
	testing.expectf(t, len(got.lines) == 2, "got {} lines", len(got.lines))
	if len(got.lines) == 2 {
		testing.expect_value(t, got.lines[0], "alpha")
		testing.expect_value(t, got.lines[1], "beta")
	}
}

@(test)
test_clear_truncates :: proc(t: ^testing.T) {
	dir := scratch(t, "clear")
	defer {
		_ = os.remove_all(dir)
		delete(dir)
	}
	path := fmt.tprintf("{}/app.log", dir)
	write_file(t, path, "something\n")

	testing.expect_value(t, clear(path), "")
	info, err := os.stat(path, context.temp_allocator)
	testing.expectf(t, err == nil, "stat failed: {}", err)
	testing.expect_value(t, info.size, 0)

	// Clearing a file that is not there is a no-op, not an error.
	testing.expect_value(t, clear(fmt.tprintf("{}/absent.log", dir)), "")
}

// rotate is the part with the sharp edge: the service keeps writing into the
// same inode, so the old bytes must leave through a copy and the original has to
// stay in place, truncated.
@(test)
test_rotate_copies_and_truncates :: proc(t: ^testing.T) {
	dir := scratch(t, "rotate")
	defer {
		_ = os.remove_all(dir)
		delete(dir)
	}
	path := fmt.tprintf("{}/svc.log", dir)
	write_file(t, path, "yesterday\n")

	r: Rotator
	rotator_init(&r)
	defer rotator_destroy(&r)

	// Prime the rotator with "today" so the file is not rotated on first sight.
	now := time.now()
	_, err := rotate(&r, path, now)
	testing.expectf(t, err == "", "first pass must be a no-op: {}", err)

	tomorrow := time.time_add(now, 24 * time.Hour)
	moved, rerr := rotate(&r, path, tomorrow)
	defer delete(moved)
	testing.expectf(t, rerr == "", "rotate failed: {}", rerr)
	testing.expect(t, len(moved) > len(path), "rotated name must carry the day suffix")

	old, old_err := os.read_entire_file_from_path(moved, context.allocator)
	defer delete(old)
	testing.expectf(t, old_err == nil, "cannot read rotated file: {}", old_err)
	testing.expect_value(t, string(old), "yesterday\n")

	info, serr := os.stat(path, context.temp_allocator)
	testing.expectf(t, serr == nil, "stat failed: {}", serr)
	testing.expect_value(t, info.size, 0)

	// A second pass on the same day does nothing: the day is already recorded.
	_, again := rotate(&r, path, tomorrow)
	testing.expect_value(t, again, "")
}

@(test)
test_rotate_skips_missing_and_special :: proc(t: ^testing.T) {
	r: Rotator
	rotator_init(&r)
	defer rotator_destroy(&r)

	now := time.now()
	moved, err := rotate(&r, "/nonexistent/gorch/svc.log", now)
	testing.expect_value(t, err, "")
	testing.expect_value(t, moved, "")

	// /dev/null is a character device: truncating it would be wrong.
	moved, err = rotate(&r, "/dev/null", now)
	testing.expect_value(t, err, "")
	testing.expect_value(t, moved, "")
}

@(test)
test_pack_day_orders_by_calendar :: proc(t: ^testing.T) {
	testing.expect(t, pack_day(2026, 11, 4) > pack_day(2026, 1, 31))
	testing.expect(t, pack_day(2026, 12, 31) < pack_day(2027, 1, 1))
	testing.expect(t, pack_day(2026, 11, 4) == pack_day(2026, 11, 4))
}
