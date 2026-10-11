package cron

import "core:testing"
import "core:fmt"
import "core:strings"
import "core:sync"

// check_next parses expr and checks the next run time after `after` (UTC).
check_next :: proc(t: ^testing.T, expr: string, after: i64, want: i64, label: string) {
	s, ok := parse(expr)
	testing.expect(t, ok, fmt.tprintf("parse '{}'", expr))
	got := next_of(s, 0, after)
	testing.expect(t, got == want, fmt.tprintf("{}: next_of('{}', after) = {} want {}", label, expr, got, want))
}

// Fixed reference timestamps (UTC).
ts_1000 :: 1787997600 // 2026-08-29 10:00:00 (Saturday)

// set_arena is a no-op: tests use the default global allocator (leak reports
// are warnings only and do not fail the suite).
set_arena :: proc(_: ^testing.T) {}

@(test)
test_decompose :: proc(t: ^testing.T) {
	set_arena(t)
	y, mo, d, h, mi, s, wd := decompose(ts_1000)
	testing.expect(t, y == 2026 && mo == 8 && d == 29, fmt.tprintf("date {}-{}-{}", y, mo, d))
	testing.expect(t, h == 10 && mi == 0 && s == 0, "clock")
	testing.expect(t, wd == 6, fmt.tprintf("weekday {} want 6 (Sat, 0=Sun)", wd))

	// Sunday
	_, _, _, _, _, _, wd2 := decompose(1788048000 + 86400) // 2026-08-31 00:00 Monday? recompute below
	y2, mo2, d2, _, _, _, wd3 := decompose(1788048000)     // 2026-08-30 00:00 (Sunday)
	testing.expect(t, y2 == 2026 && mo2 == 8 && d2 == 30 && wd3 == 0, fmt.tprintf("2026-08-30 wd {}", wd3))
	_ = wd2
}

@(test)
test_parse_valid :: proc(t: ^testing.T) {
	set_arena(t)
	ok_exprs := []string{
		"* * * * * *",
		"0 0 * * * *",
		"0 30 10 * * *",
		"0 0 * * *",   // 5-field
		"*/15 * * * * *",
		"0 0 13 * 5",
		"@daily",
		"@hourly",
		"@minutely",
		"@every 5m",
		"@every 1h30m",
		"0 0 1 1 *",   // yearly style 5-field? (5 fields)
	}
	for e in ok_exprs {
		_, ok := parse(e)
		testing.expect(t, ok, fmt.tprintf("expected parse ok for '{}'", e))
	}
}

@(test)
test_parse_invalid :: proc(t: ^testing.T) {
	set_arena(t)
	bad := []string{
		"",
		"* * *",
		"1 2 3",
		"60 * * * * *",   // second out of range
		"* 60 * * * *",   // minute out of range
		"* * 24 * * *",   // hour out of range
		"* * * 32 * *",   // dom out of range
		"* * * * 13 *",   // month out of range
		"* * * * * 8",    // dow out of range
		"@every",
		"@bogus",
		"* * * * * * *", // 7 fields
	}
	for e in bad {
		_, ok := parse(e)
		testing.expect(t, !ok, fmt.tprintf("expected parse fail for '{}'", e))
	}
}

@(test)
test_next_of :: proc(t: ^testing.T) {
	set_arena(t)
	check_next(t, "* * * * * *", ts_1000, ts_1000 + 1, "every-sec")
	check_next(t, "0 0 * * * *", ts_1000, 1788001200, "hourly")         // 2026-08-29 11:00:00
	check_next(t, "0 30 10 * * *", ts_1000, 1787999400, "10:30 today")  // 2026-08-29 10:30:00
	check_next(t, "0 30 10 * * *", 1787999400, 1788085800, "10:30 tmrw") // 2026-08-30 10:30:00
	check_next(t, "0 0 * * *", ts_1000, 1788048000, "daily-midnight")   // 2026-08-30 00:00:00
	check_next(t, "@daily", ts_1000, 1788048000, "descriptor daily")
	check_next(t, "@every 5m", ts_1000, ts_1000 + 300, "every 5m")
	check_next(t, "*/15 * * * * *", ts_1000, ts_1000 + 15, "sec step")

	// dow Monday (0=Sunday) -> next Monday 2026-08-31 00:00 = 1788134400
	check_next(t, "0 0 * * 1", ts_1000, 1788134400, "monday")
	// Vixie OR: dom 13 OR dow 5(Fri). First Friday after 08-29 is 09-04 00:00
	check_next(t, "0 0 13 * 5", ts_1000, 1788480000, "dom-or-dow")
}

@(test)
test_parse_duration :: proc(t: ^testing.T) {
	set_arena(t)
	testing.expect(t, parse_duration_secs("5s") == 5)
	testing.expect(t, parse_duration_secs("2m") == 120)
	testing.expect(t, parse_duration_secs("1h30m") == 5400)
	testing.expect(t, parse_duration_secs("1d") == 86400)
	testing.expect(t, parse_duration_secs("45") == -1) // no unit
	testing.expect(t, parse_duration_secs("abc") == -1)
}

@(test)
test_history_trim :: proc(t: ^testing.T) {
	set_arena(t)
	s := new_scheduler()
	for i in 0..<15 {
		rec := CronExecutionRecord{
			service    = strings.clone("svc"),
			started_at = i64(i),
			status     = strings.clone("success"),
		}
		record_execution(s, "svc", rec)
	}
	h := get_history(s, "svc")
	testing.expect(t, len(h) == max_history, fmt.tprintf("history len {}", len(h)))
	if len(h) > 0 {
		// last record should be the most recent (started_at == 14)
		testing.expect(t, h[len(h)-1].started_at == 14, fmt.tprintf("last started_at {}", h[len(h)-1].started_at))
		testing.expect(t, h[0].started_at == 5, fmt.tprintf("first kept started_at {}", h[0].started_at))
	}
}

// mutable clock for scheduler firing tests.
test_clock: i64 = ts_1000
test_count := 0
test_mu: sync.Mutex

fake_now :: proc() -> i64 { return test_clock }
zero_offset :: proc(_: i64) -> i64 { return 0 }
count_fn :: proc(_: rawptr) {
	sync.mutex_lock(&test_mu)
	test_count += 1
	sync.mutex_unlock(&test_mu)
}

@(test)
test_catchup_collapses :: proc(t: ^testing.T) {
	set_arena(t)
	test_clock = 1787983200 // 2026-08-29 06:00:00
	test_count = 0

	s := new_scheduler()
	s.now_fn = fake_now
	s.offset_fn = zero_offset

	ok := add_job(s, "up", "0 0 * * * *", "", count_fn, nil)
	testing.expect(t, ok)
	// next should be 07:00
	testing.expect(t, job_next(s, "up") == 1787986800, fmt.tprintf("next {}", job_next(s, "up")))

	// Machine asleep 06:00 -> 15:30; hours 07..15 all missed -> one catch-up run.
	test_clock = 1788017400 // 2026-08-29 15:30:00
	run_due(s, test_clock)
	sync.wait_group_wait(&s.wg)

	testing.expect(t, test_count == 1, fmt.tprintf("catch-up fires {} want 1", test_count))
	// rescheduled strictly in the future: 16:00
	testing.expect(t, job_next(s, "up") == 1788019200, fmt.tprintf("resched next {}", job_next(s, "up")))

	// Another tick at same instant must not re-fire.
	run_due(s, test_clock)
	sync.wait_group_wait(&s.wg)
	testing.expect(t, test_count == 1, fmt.tprintf("no double fire, got {}", test_count))
}

@(test)
test_same_expr_preserves_next :: proc(t: ^testing.T) {
	set_arena(t)
	test_clock = ts_1000
	s := new_scheduler()
	s.now_fn = fake_now
	s.offset_fn = zero_offset

	ok := add_job(s, "j", "0 30 10 * * *", "", count_fn, nil)
	testing.expect(t, ok)
	first := job_next(s, "j")

	// Simulate a rebuild that lands just before the due time: clock advances
	// but stays before `first`; re-adding the SAME schedule must not push next.
	test_clock = first - 60
	ok2 := add_job(s, "j", "0 30 10 * * *", "", count_fn, nil)
	testing.expect(t, ok2)
	testing.expect(t, job_next(s, "j") == first, "unchanged schedule must keep next")

	// Changing the expression DOES recompute next.
	ok3 := add_job(s, "j", "0 0 3 * * *", "", count_fn, nil)
	testing.expect(t, ok3)
	testing.expect(t, job_next(s, "j") != first, "changed schedule recomputes next")
}
