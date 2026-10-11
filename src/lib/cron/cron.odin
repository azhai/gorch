package cron

import "base:runtime"
import "core:mem"
import "core:time"
import "core:sync"
import "core:thread"
import "core:strings"

// NOTE(threading): @(test) procs run with a NON-thread-safe per-test arena as
// the default allocator. The scheduler spawns worker threads (run_due ->
// thread.run_with_data) that allocate/free through the ambient context and
// mutate the Scheduler's maps. To avoid corrupting that arena (SIGABRT), every
// allocation that can be touched from a worker thread is pinned to the
// thread-safe global heap allocator via `context.allocator =
// runtime.default_allocator()` at the top of new_scheduler, run_due and
// fire_dispatch. (Odin's `context` is copied per proc call, so this must be set
// inline in each proc rather than via a helper proc.)

// ---------------------------------------------------------------------------
// Public types mirror the Go cron.Scheduler surface used by the supervisor:
//   new_scheduler, add_job, start, stop, run_due, job_next, job_count,
//   remove_job, has_job, job_names, record_execution, get_history, parse.
// ---------------------------------------------------------------------------

tick_interval := 250 * time.Millisecond
max_history :: 10

// CronExecutionRecord is a single cron run entry retained per service.
CronExecutionRecord :: struct {
	service:    string,
	started_at: i64, // unix seconds
	ended_at:   ^i64,
	exit_code:  ^int,
	status:     string,
	pid:        int,
}

Schedule_Kind :: enum {
	Spec,
	Every,
}

Field :: struct {
	bits: u64,
	star: bool,
}

field_has :: proc(f: Field, v: int) -> bool {
	return (f.bits >> u64(v)) & 1 != 0
}

Schedule :: struct {
	kind:   Schedule_Kind,
	second: Field,
	minute: Field,
	hour:   Field,
	dom:    Field,
	month:  Field,
	dow:    Field,
	every:  i64, // seconds, for .Every
}

Job :: struct {
	name:       string,
	expression: string,
	timezone:   string,
	schedule:   Schedule,
	fn:         proc(user: rawptr),
	user:       rawptr,
	next_unix:  i64, // -1 when the schedule never fires again
}

// Scheduler drives cron jobs from a wall-clock poll loop (catch-up on sleep).
Scheduler :: struct {
	mu:      sync.RW_Mutex,
	jobs:    map[string]^Job,
	records: map[string][]CronExecutionRecord,
	running: map[string]bool,

	loop_mu:        sync.Mutex,
	started:        bool,
	stop_requested: bool,
	loop_thread:    ^thread.Thread,
	wg:             sync.Wait_Group,

	// now_fn returns the current wall clock in unix seconds (test override).
	now_fn: proc() -> i64,
	// offset_fn returns the local UTC offset (seconds) applied to wall-clock
	// matching. Defaults to the machine's local timezone offset.
	offset_fn: proc(unix: i64) -> i64,
}

// ---------------------------------------------------------------------------
// Proleptic-Gregorian calendar helpers (epoch days <-> civil). These operate
// purely on integers so cron matching is DST-independent once an offset is
// chosen, and are trivially unit-testable.
// ---------------------------------------------------------------------------

floor_div :: proc(a, b: i64) -> i64 {
	q := a / b
	if (a % b != 0) && ((a < 0) != (b < 0)) {
		q -= 1
	}
	return q
}

days_from_civil :: proc(y: int, m: int, d: int) -> i64 {
	yy := i64(y)
	mm := i64(m)
	dd := i64(d)
	if mm <= 2 {
		yy -= 1
	}
	era_num := yy
	if yy < 0 {
		era_num = yy - 399
	}
	era := era_num / 400
	yoe := yy - era*400 // [0,399]
	mp := mm - 3
	if mm <= 2 {
		mp = mm + 9
	}
	doy := (153*mp + 2) / 5 + dd - 1 // [0,365]
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

// civil_from_days -> (year, month, day)
civil_from_days :: proc(z: i64) -> (year: int, month: int, day: int) {
	zz := z + 719468
	era_num := zz
	if zz < 0 {
		era_num = zz - 146096
	}
	era := era_num / 146097
	doe := zz - era*146097 // [0,146096]
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365 // [0,399]
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100) // [0,365]
	mp := (5*doy + 2) / 153                   // [0,11]
	d := doy - (153*mp+2)/5 + 1               // [1,31]
	m: i64 = 0
	if mp < 10 {
		m = mp + 3
	} else {
		m = mp - 9
	}
	if m <= 2 {
		y += 1
	}
	return int(y), int(m), int(d)
}

// decompose breaks local epoch seconds into calendar + clock fields.
// weekday uses 0=Sunday..6=Saturday.
decompose :: proc(local: i64) -> (y, mo, d, h, mi, sec, wd: int) {
	days := floor_div(local, 86400)
	sod := local - days*86400 // [0,86399]
	h = int(sod / 3600)
	mi = int((sod % 3600) / 60)
	sec = int(sod % 60)
	y, mo, d = civil_from_days(days)
	wd = int(((days % 7) + 4) % 7) // day 0 == Thursday == 4
	return
}

// ---------------------------------------------------------------------------
// Wall-clock offset. default_offset is UTC (0); a caller may inject an
// offset_fn to schedule against a local timezone (matching Go's time.Local).
// ---------------------------------------------------------------------------

default_offset :: proc(unix: i64) -> i64 {
	_ = unix
	return 0
}

default_now :: proc() -> i64 {
	return time.to_unix_seconds(time.now())
}

// ---------------------------------------------------------------------------
// Expression parsing (robfig standard parser with seconds, plus descriptors).
// ---------------------------------------------------------------------------

stepped :: proc(lo, hi, step: int) -> u64 {
	bits := u64(0)
	v := lo
	for v <= hi {
		bits |= u64(1) << u64(v)
		v += step
	}
	return bits
}

parse_int :: proc(s: string, out: ^int) -> bool {
	if len(s) == 0 {
		return false
	}
	n := 0
	for c in s {
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c - '0')
		if n > 1_000_000 {
			return false
		}
	}
	out^ = n
	return true
}

// parse_token handles "*", "*/n", "a", "a-b", "a-b/n".
parse_token :: proc(tok: string, lo, hi: int) -> (bits: u64, star: bool, ok: bool) {
	if len(tok) == 0 {
		return 0, false, false
	}
	step := 1
	range_part := tok
	slash := strings.index(tok, "/")
	if slash >= 0 {
		range_part = tok[0:slash]
		sv := 0
		if !parse_int(tok[slash+1:], &sv) || sv <= 0 {
			return 0, false, false
		}
		step = sv
	}

	if range_part == "*" {
		return stepped(lo, hi, step), true, true
	}

	rlo := 0
	rhi := 0
	dash := strings.index(range_part, "-")
	if dash >= 0 {
		if !parse_int(range_part[0:dash], &rlo) {
			return 0, false, false
		}
		if !parse_int(range_part[dash+1:], &rhi) {
			return 0, false, false
		}
	} else {
		if !parse_int(range_part, &rlo) {
			return 0, false, false
		}
		rhi = rlo
	}
	if rlo < lo || rhi > hi || rlo > rhi {
		return 0, false, false
	}
	return stepped(rlo, rhi, step), false, true
}

// parse_field parses a comma-separated spec into a bit set.
parse_field :: proc(spec: string, lo, hi: int) -> (f: Field, ok: bool) {
	parts := strings.split(spec, ",")
	bits := u64(0)
	star := false
	for p in parts {
		b, s, pok := parse_token(p, lo, hi)
		if !pok {
			delete(parts)
			return Field{}, false
		}
		bits |= b
		star = star || s
	}
	delete(parts)
	f.bits = bits
	f.star = star
	return f, true
}

// parse_duration_secs parses a Go-style subset duration ("5s","2m","1h30m","1d")
// into total seconds. Returns -1 on failure.
parse_duration_secs :: proc(s: string) -> i64 {
	if len(s) == 0 {
		return -1
	}
	n := len(s)
	total := i64(0)
	i := 0
	saw_any := false
	for i < n {
		start := i
		for i < n && s[i] >= u8('0') && s[i] <= u8('9') {
			i += 1
		}
		if i == start {
			return -1
		}
		num := 0
		for j := start; j < i; j += 1 {
			num = num*10 + int(s[j]) - int(u8('0'))
		}
		if i >= n {
			return -1 // number without unit
		}
		unit := s[i]
		i += 1
		mult := i64(0)
		if unit == u8('s') {
			mult = 1
		} else if unit == u8('m') {
			mult = 60
		} else if unit == u8('h') {
			mult = 3600
		} else if unit == u8('d') {
			mult = 86400
		} else {
			return -1
		}
		total += i64(num) * mult
		saw_any = true
	}
	if !saw_any {
		return -1
	}
	return total
}

// expand_descriptor maps "@name" predefined schedules to a 5-field cron spec
// (minute granularity, seconds default to 0). Returns "" if unknown.
expand_descriptor :: proc(expr: string) -> string {
	switch expr {
	case "@yearly", "@annually":
		return "0 0 1 1 *"
	case "@monthly":
		return "0 0 1 * *"
	case "@weekly":
		return "0 0 0 * 0"
	case "@daily", "@midnight":
		return "0 0 * * *"
	case "@hourly":
		return "0 * * * *"
	case "@minutely":
		return "* * * * *"
	}
	return ""
}

// split_ws tokenises on ASCII whitespace runs, returning subslices of `s`
// (no allocation; the caller owns `s` for the duration).
split_ws :: proc(s: string) -> []string {
	out: [dynamic]string
	i := 0
	n := len(s)
	for i < n {
		for i < n && (s[i] == ' ' || s[i] == '\t') {
			i += 1
		}
		start := i
		for i < n && s[i] != ' ' && s[i] != '\t' {
			i += 1
		}
		if i > start {
			append(&out, s[start:i])
		}
	}
	if len(out) == 0 {
		delete(out)
		return nil
	}
	return out[:]
}

// parse compiles a cron expression into a Schedule. Supports 6-field
// (second minute hour dom month dow), 5-field (second defaults to 0), and
// @descriptors (@every <dur>, @hourly, @daily, ...).
parse :: proc(expr: string) -> (s: Schedule, ok: bool) {
	e := strings.trim_space(expr)
	if e == "" {
		return Schedule{}, false
	}

	if strings.has_prefix(e, "@every ") {
		dur := strings.trim_space(e[len("@every "):])
		secs := parse_duration_secs(dur)
		if secs <= 0 {
			return Schedule{}, false
		}
		return Schedule{kind = .Every, every = secs}, true
	}
	if strings.has_prefix(e, "@") {
		exp := expand_descriptor(e)
		if exp == "" {
			return Schedule{}, false
		}
		e = exp
	}

	tokens := split_ws(e)

	if len(tokens) != 5 && len(tokens) != 6 {
		return Schedule{}, false
	}

	sec_spec := "0"
	rest: []string
	if len(tokens) == 6 {
		sec_spec = tokens[0]
		rest = tokens[1:]
	} else {
		rest = tokens
	}

	if len(rest) != 5 {
		return Schedule{}, false
	}

	secf, e1 := parse_field(sec_spec, 0, 59)
	minf, e2 := parse_field(rest[0], 0, 59)
	hourf, e3 := parse_field(rest[1], 0, 23)
	domf, e4 := parse_field(rest[2], 1, 31)
	monthf, e5 := parse_field(rest[3], 1, 12)
	dowf, e6 := parse_field(rest[4], 0, 7) // allow 7 == Sunday
	if !e1 || !e2 || !e3 || !e4 || !e5 || !e6 {
		return Schedule{}, false
	}

	// Normalise dow 7 -> 0 (Sunday).
	if (dowf.bits >> 7) & 1 != 0 {
		dowf.bits |= u64(1) << 0
		dowf.bits &= ~(u64(1) << 7)
	}

	if secf.bits == 0 || minf.bits == 0 || hourf.bits == 0 || domf.bits == 0 || monthf.bits == 0 || dowf.bits == 0 {
		return Schedule{}, false
	}

	return Schedule{
		kind   = .Spec,
		second = secf,
		minute = minf,
		hour   = hourf,
		dom    = domf,
		month  = monthf,
		dow    = dowf,
	}, true
}

// day_match implements the Vixie-cron dom/dow rule.
day_match :: proc(s: Schedule, day, wd: int) -> bool {
	dom_match := s.dom.star || field_has(s.dom, day)
	dow_match := s.dow.star || field_has(s.dow, wd)
	if s.dom.star && s.dow.star {
		return true
	}
	if s.dom.star {
		return dow_match
	}
	if s.dow.star {
		return dom_match
	}
	// both restricted: OR
	return dom_match || dow_match
}

// next_of computes the next run time (unix seconds) strictly after `after`.
// Returns -1 when there is no match within the 5-year horizon.
next_of :: proc(s: Schedule, offset: i64, after: i64) -> i64 {
	if s.kind == .Every {
		return after + s.every
	}
	local := after + offset
	t := local + 1
	start_year, _, _ := civil_from_days(floor_div(t, 86400))
	limit_year := start_year + 5

	for _ in 0..<1_000_000 {
		y, mo, d, h, mi, sec, wd := decompose(t)
		if y > limit_year {
			return -1
		}
		if !field_has(s.month, mo) {
			ny := y
			nmo := mo + 1
			if nmo > 12 {
				nmo = 1
				ny += 1
			}
			t = days_from_civil(ny, nmo, 1) * 86400
			continue
		}
		if !day_match(s, d, wd) {
			midnight := floor_div(t, 86400) * 86400
			t = midnight + 86400
			continue
		}
		if !field_has(s.hour, h) {
			hour_start := floor_div(t, 3600) * 3600
			t = hour_start + 3600
			continue
		}
		if !field_has(s.minute, mi) {
			min_start := floor_div(t, 60) * 60
			t = min_start + 60
			continue
		}
		if !field_has(s.second, sec) {
			t += 1
			continue
		}
		return t - offset
	}
	return -1
}

// ---------------------------------------------------------------------------
// Scheduler lifecycle & API
// ---------------------------------------------------------------------------

new_scheduler :: proc() -> ^Scheduler {
	// Maps and the Scheduler itself are shared with worker threads; allocate
	// them from the thread-safe global heap allocator (not the ambient arena).
	context.allocator = runtime.default_allocator()
	s := new(Scheduler)
	s.jobs = make(map[string]^Job)
	s.records = make(map[string][]CronExecutionRecord)
	s.running = make(map[string]bool)
	s.now_fn = default_now
	s.offset_fn = default_offset
	return s
}

now_unix :: proc(s: ^Scheduler) -> i64 {
	if s.now_fn != nil {
		return s.now_fn()
	}
	return default_now()
}

// add_job registers or replaces a cron job. A bad expression never mutates
// existing state; re-registering an unchanged schedule preserves `next`.
add_job :: proc(s: ^Scheduler, name, expression, tz: string, fn: proc(user: rawptr), user: rawptr) -> bool {
	sched, ok := parse(expression)
	if !ok {
		return false
	}
	now := now_unix(s)
	offset: i64 = 0
	if s.offset_fn != nil {
		offset = s.offset_fn(now)
	}
	next := next_of(sched, offset, now)

	sync.rw_mutex_lock(&s.mu)
	defer sync.rw_mutex_unlock(&s.mu)

	if old, ex := s.jobs[name]; ex && old.expression == expression && old.timezone == tz {
		old.schedule = sched
		old.fn = fn
		old.user = user
		return true
	}
	j := new(Job)
	j.name = strings.clone(name)
	j.expression = strings.clone(expression)
	j.timezone = strings.clone(tz)
	j.schedule = sched
	j.fn = fn
	j.user = user
	j.next_unix = next
	s.jobs[name] = j
	return true
}

// remove_job drops a job by name; returns whether it existed.
remove_job :: proc(s: ^Scheduler, name: string) -> bool {
	sync.rw_mutex_lock(&s.mu)
	defer sync.rw_mutex_unlock(&s.mu)
	if _, ok := s.jobs[name]; !ok {
		return false
	}
	delete_key(&s.jobs, name)
	return true
}

has_job :: proc(s: ^Scheduler, name: string) -> bool {
	sync.rw_mutex_shared_lock(&s.mu)
	defer sync.rw_mutex_shared_unlock(&s.mu)
	_, ok := s.jobs[name]
	return ok
}

job_count :: proc(s: ^Scheduler) -> int {
	sync.rw_mutex_shared_lock(&s.mu)
	defer sync.rw_mutex_shared_unlock(&s.mu)
	return len(s.jobs)
}

job_names :: proc(s: ^Scheduler) -> []string {
	sync.rw_mutex_shared_lock(&s.mu)
	defer sync.rw_mutex_shared_unlock(&s.mu)
	out: [dynamic]string
	for name, _ in s.jobs {
		append(&out, strings.clone(name))
	}
	return out[:]
}

job_next :: proc(s: ^Scheduler, name: string) -> i64 {
	sync.rw_mutex_shared_lock(&s.mu)
	defer sync.rw_mutex_shared_unlock(&s.mu)
	if j, ok := s.jobs[name]; ok {
		return j.next_unix
	}
	return -1
}

// start launches the run loop (idempotent).
start :: proc(s: ^Scheduler) {
	sync.mutex_lock(&s.loop_mu)
	defer sync.mutex_unlock(&s.loop_mu)
	if s.started {
		return
	}
	s.started = true
	s.stop_requested = false
	s.loop_thread = thread.create_and_start_with_data(rawptr(s), run_loop_fn, nil, .Normal, true, "cron-loop")
}

// stop halts the run loop and waits for in-flight jobs.
stop :: proc(s: ^Scheduler) {
	sync.mutex_lock(&s.loop_mu)
	if !s.started {
		sync.mutex_unlock(&s.loop_mu)
		return
	}
	s.started = false
	s.stop_requested = true
	sync.mutex_unlock(&s.loop_mu)
	sync.wait_group_wait(&s.wg)
}

run_loop_fn :: proc(p: rawptr) {
	s := cast(^Scheduler)p
	for {
		sync.mutex_lock(&s.loop_mu)
		stopped := s.stop_requested
		sync.mutex_unlock(&s.loop_mu)
		if stopped {
			return
		}
		run_due(s, now_unix(s))
		time.sleep(tick_interval)
	}
}

// run_due fires every job whose next time has arrived, then reschedules.
// Missed occurrences collapse into a single catch-up run.
run_due :: proc(s: ^Scheduler, now: i64) {
	// Fire_Arg and `due` cross the thread boundary; use the global allocator.
	context.allocator = runtime.default_allocator()
	sync.rw_mutex_lock(&s.mu)
	due: [dynamic]^Job
	offset: i64 = 0
	if s.offset_fn != nil {
		offset = s.offset_fn(now)
	}
	for _, j in s.jobs {
		if j.next_unix < 0 || now < j.next_unix {
			continue
		}
		j.next_unix = next_of(j.schedule, offset, now)
		append(&due, j)
	}
	sync.rw_mutex_unlock(&s.mu)

	for j in due {
		sync.wait_group_add(&s.wg, 1)
		arg := new(Fire_Arg)
		arg.s = s
		arg.j = j
		thread.run_with_data(rawptr(arg), fire_dispatch)
	}
	delete(due)
}

Fire_Arg :: struct {
	s: ^Scheduler,
	j: ^Job,
}

fire_dispatch :: proc(p: rawptr) {
	// Runs on a worker thread: pin to the global allocator so free(arg) matches
	// the run_due allocation and any map/string work stays thread-safe.
	context.allocator = runtime.default_allocator()
	arg := cast(^Fire_Arg)p
	defer free(arg)
	fire(arg.s, arg.j)
}

// fire runs a single job, recording an "overlap" and skipping if a previous
// run is still in progress.
fire :: proc(s: ^Scheduler, j: ^Job) {
	defer sync.wait_group_done(&s.wg)

	sync.rw_mutex_lock(&s.mu)
	if r, ok := s.running[j.name]; ok && r {
		sync.rw_mutex_unlock(&s.mu)
		rec := CronExecutionRecord{
			service    = strings.clone(j.name),
			started_at = now_unix(s),
			status     = strings.clone("overlap"),
		}
		record_execution(s, j.name, rec)
		return
	}
	s.running[j.name] = true
	sync.rw_mutex_unlock(&s.mu)

	j.fn(j.user)

	sync.rw_mutex_lock(&s.mu)
	delete_key(&s.running, j.name)
	sync.rw_mutex_unlock(&s.mu)
}

// record_execution appends a run record, retaining the most recent max_history.
record_execution :: proc(s: ^Scheduler, name: string, rec: CronExecutionRecord) {
	sync.rw_mutex_lock(&s.mu)
	defer sync.rw_mutex_unlock(&s.mu)
	old := s.records[name]
	tmp := make([]CronExecutionRecord, len(old)+1)
	for idx in 0..<len(old) {
		tmp[idx] = old[idx]
	}
	tmp[len(old)] = rec
	if len(tmp) > max_history {
		tmp = tmp[len(tmp)-max_history:]
	}
	s.records[name] = tmp
}

get_history :: proc(s: ^Scheduler, name: string) -> []CronExecutionRecord {
	sync.rw_mutex_shared_lock(&s.mu)
	defer sync.rw_mutex_shared_unlock(&s.mu)
	recs, ok := s.records[name]
	if !ok {
		return []CronExecutionRecord{}
	}
	out := make([]CronExecutionRecord, len(recs))
	if len(recs) > 0 {
		mem.copy(raw_data(out), raw_data(recs), len(recs)*size_of(CronExecutionRecord))
	}
	return out
}
