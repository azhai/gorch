package web

import "core:strings"
import "core:testing"
import "core:time"

import "../../lib/status"

@(test)
test_sse_interval :: proc(t: ^testing.T) {
	cases := []struct {
		raw:  string,
		want: time.Duration,
	} {
		{"5s", 5 * time.Second},
		{"1m", time.Minute},
		{"15m", 15 * time.Minute},
		{"2h", 2 * time.Hour},
		{"1500ms", 1500 * time.Millisecond},
		// Anything the SPA could send that we cannot use falls back to 5s rather
		// than stalling the stream.
		{"", 5 * time.Second},
		{"abc", 5 * time.Second},
		{"0s", 5 * time.Second},
		{"-3s", 5 * time.Second},
		{"100h", 5 * time.Second},
	}
	for c in cases {
		got := sse_interval(c.raw)
		testing.expectf(
			t,
			got == c.want,
			"sse_interval({q}) = {v}, want {w}",
			c.raw,
			got,
			c.want,
		)
	}
}

@(test)
test_status_change_frame :: proc(t: ^testing.T) {
	code := 9
	ss := status.ServiceStatus {
		name       = "alpha",
		status     = .Crashed,
		pid        = 0,
		started_at = 0,
		memory_mb  = 0,
		exit_code  = &code,
	}
	b := strings.builder_make()
	defer strings.builder_destroy(&b)

	write_status_change(&b, ss)
	frame := strings.to_string(b)

	testing.expect(t, strings.has_prefix(frame, "event: status_change\ndata: "))
	testing.expect(t, strings.has_suffix(frame, "\n\n"))
	testing.expect(t, strings.contains(frame, `"payload":`))
	testing.expect(t, strings.contains(frame, `"name":"alpha"`))
	testing.expect(t, strings.contains(frame, `"status":"crashed"`))
	testing.expect(t, strings.contains(frame, `"exitCode":9`))
}

@(test)
test_status_change_omits_absent_exit_code :: proc(t: ^testing.T) {
	ss := status.ServiceStatus{name = "delta", status = .Stopped}
	b := strings.builder_make()
	defer strings.builder_destroy(&b)

	write_status_change(&b, ss)
	frame := strings.to_string(b)

	testing.expect(t, strings.contains(frame, `"status":"stopped"`))
	testing.expect(t, !strings.contains(frame, "exitCode"))
}

@(test)
test_uptime_tick_lists_only_running :: proc(t: ^testing.T) {
	list := []status.ServiceStatus {
		{name = "alpha", status = .Running, pid = 42, started_at = 1000, memory_mb = 3.5},
		{name = "delta", status = .Stopped},
	}
	b := strings.builder_make()
	defer strings.builder_destroy(&b)

	write_uptime_tick(&b, list)
	frame := strings.to_string(b)

	testing.expect(t, strings.has_prefix(frame, "event: uptime_tick\ndata: "))
	testing.expect(t, strings.has_suffix(frame, "\n\n"))
	testing.expect(t, strings.contains(frame, `"services":{`))
	testing.expect(t, strings.contains(frame, `"alpha":{"pid":42,"memoryMB":3.5,"startedAt":1000}`))
	// A stopped service has no live process, so it must not appear: the SPA
	// reads absence as "the counter stops here".
	testing.expect(t, !strings.contains(frame, "delta"))
}

@(test)
test_uptime_tick_escapes_names :: proc(t: ^testing.T) {
	list := []status.ServiceStatus{{name = `a"b`, status = .Running, pid = 7, started_at = 5}}
	b := strings.builder_make()
	defer strings.builder_destroy(&b)

	write_uptime_tick(&b, list)
	frame := strings.to_string(b)

	// A quote in a service name must be escaped, or the frame is not JSON.
	testing.expect(t, strings.contains(frame, `"a\"b":{"pid":7`))
}

@(test)
test_service_json_reports_stopping :: proc(t: ^testing.T) {
	// A stopping service still owns a live process, so it keeps its pid, its
	// age and its memory figure. Dropping them would make the card flicker to
	// dashes during the SIGTERM window the Stop button just opened.
	ss := status.ServiceStatus {
		name       = "alpha",
		status     = .Stopping,
		pid        = 42,
		started_at = 1000,
		memory_mb  = 3.5,
	}
	status.compute_uptime(&ss)

	b := strings.builder_make()
	defer strings.builder_destroy(&b)
	write_service_json(&b, ss)
	out := strings.to_string(b)

	testing.expect(t, strings.contains(out, `"status":"stopping"`))
	testing.expect(t, strings.contains(out, `"pid":42`))
	testing.expect(t, strings.contains(out, `"startedAt":1000`))
	testing.expect(t, !strings.contains(out, `"uptime":null`), "a stopping process still has an age")
}
