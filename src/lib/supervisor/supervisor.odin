package supervisor

import "core:fmt"
import "core:os"
import "core:sync"
import "core:thread"
import "core:time"

import "../../lib/config"
import glog "../../lib/log"
import "../../lib/state"
import "../../lib/status"

// A supervisor for *external processes*. The unit of supervision is a PID: gorch
// forks the command from EXEC_CMD, reaps it with waitpid, and applies the restart
// policy from gorch.toml. Restarts are rate limited by a budget (count inside a
// window) so a service that crashes on startup is escalated to Failed instead of
// being respawned forever — the one thing a plain BACK_OFF loop cannot express.

// Policy mirrors the RESTART_POLICY strings in the config.
Policy :: enum {
	Never,
	On_Failure,
	Always,
}

policy_of :: proc(s: string) -> Policy {
	switch s {
	case "always":
		return .Always
	case "on-failure":
		return .On_Failure
	}
	return .Never
}

// Defaults. `MAX_RESTARTS` restarts inside `WINDOW` seconds are allowed; the next
// one escalates. Erlang's default intensity is 1/5s, which is too tight for
// services that need a few seconds to bind a port, so this is deliberately looser.
MAX_RESTARTS :: 5
WINDOW :: 60 // seconds
BACKOFF :: 5 // seconds, used when BACK_OFF is absent
STOP_GRACE :: 5 // seconds between SIGTERM and SIGKILL
POLL :: 500 * time.Millisecond
SAVE_EVERY :: time.Second

// Service is the runtime half of a config.ServiceConfig.
Service :: struct {
	name:         string,
	spec:         config.ServiceConfig,
	status:       status.Status,
	pid:          int,
	handle:       os.Process,
	owned:        bool, // we spawned it, so waitpid must reap it
	started_at:   i64, // unix seconds
	exited_at:    i64,
	exit_code:    int,
	restarts:     int,
	exits:        [dynamic]i64, // restart timestamps inside the budget window
	next_at:      i64, // earliest unix second the next start may happen
	kill_at:      i64, // 0 = no pending SIGKILL deadline
	want_running: bool, // false after `stop`, and after escalation
	restarting:   bool, // an operator restart is pending on the current exit
	saved:        state.Record, // last record written, to skip redundant flushes
	has_saved:    bool,
}

Supervisor :: struct {
	cfg:          ^config.Config,
	mu:           sync.RW_Mutex,
	services:     map[string]^Service,
	order:        [dynamic]string,
	store:        state.Store,
	has_store:    bool,
	max_restarts: int,
	window:       i64,
	default_back_off: int,
	now_fn:       proc() -> i64,
	halt:         bool,
}

now_seconds :: proc(sv: ^Supervisor) -> i64 {
	if sv.now_fn != nil {
		return sv.now_fn()
	}
	return time.to_unix_seconds(time.now())
}

// init builds the service table from the config and recovers whatever the last
// run left on disk. `state_path` may be empty to run without persistence.
init :: proc(sv: ^Supervisor, cfg: ^config.Config, state_path: string) -> string {
	sv.cfg = cfg
	sv.max_restarts = MAX_RESTARTS
	sv.window = WINDOW
	sv.default_back_off = BACKOFF
	sv.services = make(map[string]^Service)

	names := config.sorted_service_names(cfg)
	defer delete(names)

	for name in names {
		spec, ok := cfg.services[name]
		if !ok {
			continue
		}
		s := new(Service)
		s.name = name
		s.spec = spec
		s.status = .Stopped
		s.exits = make([dynamic]i64)
		sv.services[name] = s
		append(&sv.order, name)
	}

	if len(state_path) > 0 {
		store, loaded := state.open(state_path)
		sv.store = store
		sv.has_store = true
		if !loaded {
			glog.info("no previous state on disk, starting from scratch", "path", state_path)
		}
		recover_running(sv)
	}
	return ""
}

// recover_running adopts children that outlived gorch, and marks everything else
// as stopped. A recorded pid is only a hint: it is trusted only when the process
// is alive *and* its start time matches, otherwise pid reuse would make us kill
// an unrelated process.
@(private = "file")
recover_running :: proc(sv: ^Supervisor) {
	now := now_seconds(sv)
	for name in sv.order {
		s := sv.services[name]
		rec, ok := state.load_service(&sv.store, name)
		if !ok {
			s.status = .Stopped
			continue
		}
		s.restarts = rec.restarts
		s.exit_code = rec.exit_code
		s.started_at = rec.started_at
		s.exited_at = rec.exited_at
		if rec.exited_at > 0 && now - rec.exited_at < sv.window {
			append(&s.exits, rec.exited_at)
		}

		if rec.pid_valid && rec.pid > 0 && alive(rec.pid) {
			start, sok := proc_start_time(rec.pid)
			if sok && start >= rec.started_at - 5 && start <= rec.started_at + 5 {
				s.pid = rec.pid
				s.owned = false
				s.status = .Running
				s.want_running = true
				glog.info(
					"adopted a service that survived the restart",
					"service",
					name,
					"pid",
					rec.pid,
				)
				continue
			}
			glog.warn(
				"recorded pid is alive but was reused, not adopting",
				"service",
				name,
				"pid",
				rec.pid,
			)
		}
		s.pid = 0
		s.status = .Stopped
	}
}

// start_all brings every configured service up, in dependency order.
start_all :: proc(sv: ^Supervisor) {
	sync.rw_mutex_lock(&sv.mu)
	defer sync.rw_mutex_unlock(&sv.mu)
	for name in sv.order {
		s := sv.services[name]
		if s.pid > 0 {
			continue
		}
		s.want_running = true
		s.restarting = false
		s.next_at = 0
		if s.status == .Failed {
			s.status = .Stopped
			clear(&s.exits)
			s.restarts = 0
		}
		spawn_locked(sv, s, now_seconds(sv))
	}
}

stop_all :: proc(sv: ^Supervisor) {
	sync.rw_mutex_lock(&sv.mu)
	defer sync.rw_mutex_unlock(&sv.mu)
	now := now_seconds(sv)
	for name in sv.order {
		s := sv.services[name]
		s.want_running = false
		if s.pid > 0 {
			signal(s.pid, .SIGTERM)
			s.kill_at = now + STOP_GRACE
		}
	}
}

// start brings one service up now, clearing any escalation.
start :: proc(sv: ^Supervisor, name: string) -> string {
	sync.rw_mutex_lock(&sv.mu)
	defer sync.rw_mutex_unlock(&sv.mu)
	s, ok := sv.services[name]
	if !ok {
		return fmt.tprintf("unknown service: {}", name)
	}
	if s.pid > 0 {
		return ""
	}
	s.want_running = true
	s.restarting = false
	s.next_at = 0
	s.status = .Stopped
	clear(&s.exits)
	s.restarts = 0
	if !spawn_locked(sv, s, now_seconds(sv)) {
		return fmt.tprintf("cannot start {}: see logs", name)
	}
	return ""
}

// stop sends SIGTERM and schedules SIGKILL after the grace period; the exit is
// observed by the loop, so this never blocks on the child.
stop :: proc(sv: ^Supervisor, name: string) -> string {
	sync.rw_mutex_lock(&sv.mu)
	defer sync.rw_mutex_unlock(&sv.mu)
	s, ok := sv.services[name]
	if !ok {
		return fmt.tprintf("unknown service: {}", name)
	}
	s.want_running = false
	s.restarting = false
	if s.pid > 0 {
		signal(s.pid, .SIGTERM)
		s.kill_at = now_seconds(sv) + STOP_GRACE
	} else {
		s.status = .Stopped
		persist_locked(sv, s)
	}
	return ""
}

// restart stops the service and brings it back as soon as it is gone. A
// RESTART_CMD (a reload, like `angie -s reload`) is preferred when configured.
restart :: proc(sv: ^Supervisor, name: string) -> string {
	sync.rw_mutex_lock(&sv.mu)
	s, ok := sv.services[name]
	if !ok {
		sync.rw_mutex_unlock(&sv.mu)
		return fmt.tprintf("unknown service: {}", name)
	}
	if s.spec.restart_cmd != "" && s.pid > 0 {
		cmd := s.spec.restart_cmd
		dir := s.spec.work_dir
		sync.rw_mutex_unlock(&sv.mu)
		if !run_shell(cmd, dir, 15 * time.Second) {
			return fmt.tprintf("reload command failed for {}", name)
		}
		return ""
	}
	s.want_running = true
	s.restarting = true
	if s.pid > 0 {
		signal(s.pid, .SIGTERM)
		s.kill_at = now_seconds(sv) + STOP_GRACE
	} else {
		s.next_at = 0
		spawn_locked(sv, s, now_seconds(sv))
	}
	sync.rw_mutex_unlock(&sv.mu)
	return ""
}

// snapshot copies every service status for the web API.
snapshot :: proc(sv: ^Supervisor) -> []status.ServiceStatus {
	sync.rw_mutex_shared_lock(&sv.mu)
	defer sync.rw_mutex_shared_unlock(&sv.mu)
	out := make([]status.ServiceStatus, len(sv.order))
	for name, i in sv.order {
		s := sv.services[name]
		ss := status.ServiceStatus {
			name       = s.name,
			status     = s.status,
			pid        = s.pid,
			started_at = s.started_at,
		}
		if s.exit_code != 0 || s.status == .Crashed || s.status == .Failed {
			code := s.exit_code
			ss.exit_code = &code
		}
		status.compute_uptime(&ss)
		out[i] = ss
	}
	return out
}

// tick is one pass of the loop: reap exits, apply restart deadlines, then start
// whatever is due. It reports whether anything changed, so the caller knows when
// a state snapshot is worth writing.
tick :: proc(sv: ^Supervisor) -> bool {
	sync.rw_mutex_lock(&sv.mu)
	defer sync.rw_mutex_unlock(&sv.mu)
	now := now_seconds(sv)
	changed := false
	for name in sv.order {
		s := sv.services[name]
		if poll_locked(sv, s, now) {
			changed = true
		}
		if maybe_start_locked(sv, s, now) {
			changed = true
		}
	}
	return changed
}

@(private = "file")
poll_locked :: proc(sv: ^Supervisor, s: ^Service, now: i64) -> bool {
	changed := false
	if s.pid > 0 {
		if s.owned {
			st, err := os.process_wait(s.handle, 0)
			if err == nil && st.exited {
				on_exit_locked(sv, s, now, st.exit_code)
				s.pid = 0
				s.owned = false
				changed = true
			}
		} else if !alive(s.pid) {
			// Adopted process: no handle, so the exit code is unknown (-1).
			on_exit_locked(sv, s, now, -1)
			s.pid = 0
			changed = true
		}

		if s.pid > 0 && s.kill_at > 0 && now >= s.kill_at {
			signal(s.pid, .SIGKILL)
			s.kill_at = 0
			changed = true
		}
		if s.pid > 0 && s.status == .Starting {
			s.status = .Running
			changed = true
		}
	}
	if changed {
		persist_locked(sv, s)
	}
	return changed
}

@(private = "file")
on_exit_locked :: proc(sv: ^Supervisor, s: ^Service, now: i64, exit_code: int) {
	s.exited_at = now
	s.exit_code = exit_code
	s.kill_at = 0

	// An operator asked for a restart: come back immediately, budget untouched.
	if s.restarting {
		s.restarting = false
		clear(&s.exits)
		s.restarts = 0
		s.next_at = now
		s.status = .Stopped
		return
	}
	if !s.want_running {
		s.status = .Stopped
		return
	}

	policy := policy_of(s.spec.restart_policy)
	should := policy == .Always || (policy == .On_Failure && exit_code != 0)
	if !should {
		// Nobody wants it back: clear the flag, otherwise the next tick would
		// treat the empty slot as "start me" and respawn it forever.
		s.status = .Stopped
		s.want_running = false
		return
	}

	// Budget: keep only the restarts inside the window, then decide.
	for len(s.exits) > 0 && now - s.exits[0] > sv.window {
		ordered_remove(&s.exits, 0)
	}
	append(&s.exits, now)
	s.restarts = len(s.exits)
	if len(s.exits) > sv.max_restarts {
		s.status = .Failed
		s.want_running = false
		glog.error(
			"service exceeded its restart budget, giving up",
			"service",
			s.name,
			"restarts",
			len(s.exits),
			"window_s",
			sv.window,
			"exit_code",
			exit_code,
		)
		return
	}

	s.status = .Crashed
	back_off := s.spec.back_off
	if back_off <= 0 {
		back_off = sv.default_back_off
	}
	s.next_at = now + i64(back_off)
	glog.warn(
		"service exited, restarting later",
		"service",
		s.name,
		"exit_code",
		exit_code,
		"restarts",
		len(s.exits),
		"in_s",
		back_off,
	)
}

@(private = "file")
maybe_start_locked :: proc(sv: ^Supervisor, s: ^Service, now: i64) -> bool {
	if s.pid > 0 || !s.want_running || s.status == .Failed {
		return false
	}
	if now < s.next_at {
		return false
	}
	return spawn_locked(sv, s, now)
}

@(private = "file")
spawn_locked :: proc(sv: ^Supervisor, s: ^Service, now: i64) -> bool {
	if s.spec.exec_cmd == "" {
		s.status = .Failed
		glog.error("service has no EXEC_CMD", "service", s.name)
		return false
	}
	if s.spec.pre_action != "" {
		if !run_shell(s.spec.pre_action, s.spec.work_dir, 15 * time.Second) {
			glog.warn("PRE_ACTION failed, starting anyway", "service", s.name)
		}
	}

	// Both files must stay open until the child has been spawned. `defer` here
	// would run at the end of the `if` block, closing the fd before fork/exec hands
	// it to the child - which then inherits a stale or reused descriptor.
	stdout_file, stderr_file: ^os.File
	if s.spec.stdout != "" {
		if f, err := os.open(s.spec.stdout, {.Write, .Create, .Append}); err == nil {
			stdout_file = f
		} else {
			glog.warn("cannot open STDOUT", "service", s.name, "path", s.spec.stdout)
		}
	}
	if s.spec.stderr != "" {
		if f, err := os.open(s.spec.stderr, {.Write, .Create, .Append}); err == nil {
			stderr_file = f
		} else {
			glog.warn("cannot open STDERR", "service", s.name, "path", s.spec.stderr)
		}
	}

	cmd_buf: [COMMAND_MAX]u8
	command, cmd_ok := wrap_command(cmd_buf[:], s.spec.exec_cmd)
	if !cmd_ok {
		s.status = .Failed
		glog.error("EXEC_CMD is too long", "service", s.name, "max", COMMAND_MAX)
		return false
	}

	desc := os.Process_Desc {
		working_dir = s.spec.work_dir,
		command     = []string{"/bin/sh", "-c", command},
		stdout      = stdout_file,
		stderr      = stderr_file,
	}
	p, err := os.process_start(desc)
	if stdout_file != nil {
		os.close(stdout_file)
	}
	if stderr_file != nil {
		os.close(stderr_file)
	}
	if err != nil {
		s.status = .Failed
		glog.error(
			"cannot start service",
			"service",
			s.name,
			"cmd",
			s.spec.exec_cmd,
			"err",
			fmt.tprintf("{}", err),
		)
		return false
	}

	s.handle = p
	s.pid = p.pid
	s.owned = true
	s.started_at = now
	s.exited_at = 0
	s.exit_code = 0
	s.status = .Starting
	s.kill_at = 0
	s.next_at = 0
	glog.info("service started", "service", s.name, "pid", s.pid, "cmd", s.spec.exec_cmd)
	persist_locked(sv, s)
	return true
}

@(private = "file")
persist_locked :: proc(sv: ^Supervisor, s: ^Service) {
	if !sv.has_store {
		return
	}
	rec := state.Record {
			status = u8(s.status),
			pid = s.pid,
			started_at = s.started_at,
			exited_at = s.exited_at,
			exit_code = s.exit_code,
		restarts = s.restarts,
		pid_valid = s.pid > 0,
	}
	// Skip writes when nothing changed, then snapshot immediately: a transition
	// (start, exit, escalation) must survive a crash even if it lands the moment
	// after it happened.
	if s.has_saved && rec == s.saved {
		return
	}
	_ = state.save_service(&sv.store, s.name, rec)
	_ = state.save(&sv.store)
	s.saved = rec
	s.has_saved = true
}

// flush writes the snapshot to disk now. Cheap enough to call on every change.
flush :: proc(sv: ^Supervisor) -> bool {
	if !sv.has_store {
		return false
	}
	return state.save(&sv.store)
}

// shutdown stops everything, snapshots the state and frees the store. Named
// shutdown rather than close so it does not shadow the builtin in this package.
shutdown :: proc(sv: ^Supervisor) {
	stop_all(sv)
	deadline := time.now()
	for time.since(deadline) < 5 * time.Second {
		if !tick(sv) {
			break
		}
		time.sleep(50 * time.Millisecond)
	}
	_ = flush(sv)
	state.close(&sv.store)
	sv.has_store = false
}

// destroy frees everything init allocated. shutdown only stops processes, so a
// long-lived gorch never needs this; tests and embedders do, and without it the
// leak tracker reports the whole service table.
destroy :: proc(sv: ^Supervisor) {
	if sv.has_store {
		state.close(&sv.store)
		sv.has_store = false
	}
	for _, s in sv.services {
		delete(s.exits)
		free(s)
	}
	delete(sv.services)
	delete(sv.order)
	sv.services = nil
	sv.order = nil
	sv.cfg = nil
}

// Loop runs the supervisor until halt is set. Meant to be the body of a thread.
Loop :: struct {
	sv:     ^Supervisor,
	halted: u32,
}

// spawn_loop starts the tick loop on its own thread.
spawn_loop :: proc(sv: ^Supervisor) -> ^Loop {
	l := new(Loop)
	l.sv = sv
	thread.create_and_start_with_poly_data(l, loop_run, context, .Normal, true)
	return l
}

// halt_loop asks the loop to return; the caller still owns the Supervisor.
halt_loop :: proc(l: ^Loop) {
	if l != nil {
		sync.atomic_store(&l.halted, 1)
	}
}

loop_run :: proc(l: ^Loop) {
	for sync.atomic_load(&l.halted) == 0 {
		changed := tick(l.sv)
		if changed {
			_ = flush(l.sv)
		} else if l.sv.has_store {
			_ = state.save_if_due(&l.sv.store, SAVE_EVERY)
		}
		time.sleep(POLL)
	}
}
