package supervisor

import "base:runtime"
import "core:fmt"
import "core:os"
import "core:strings"
import "core:sync"
import "core:time"

import "../../lib/config"
import "../../lib/cron"
import glog "../../lib/log"
import "../../lib/logfile"
import "../../lib/state"
import "../../lib/status"
import "../../lib/worker"

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
ROTATE_EVERY :: 60 // seconds; a daemon log only needs to roll over to the minute
MEM_EVERY :: 5 // seconds between RSS samples; the reads are cheap but not free
// How long a freshly started service may take to reveal its daemon master,
// through PID_FILE or by reparenting itself to init. Matches the Go build's
// readUserPidFile window, but probed from the loop instead of blocking it.
DAEMON_PROBE :: 5

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
	memory_mb:    f64, // last sampled RSS of the pid tree, in MB
	restarts:     int,
	exits:        [dynamic]i64, // restart timestamps inside the budget window
	next_at:      i64, // earliest unix second the next start may happen
	kill_at:      i64, // 0 = no pending SIGKILL deadline
	want_running: bool, // false after `stop`, and after escalation
	restarting:   bool, // an operator restart is pending on the current exit
	// Daemonized services (nginx/angie) need a short window after spawn to
	// publish their real master pid; see daemon.odin.
	daemon_probe_until: i64,
	daemon_probe_done:  bool,
	saved:        state.Record, // last record written, to skip redundant flushes
	has_saved:    bool,
	// A scheduled (CRON) service has no resident pid; these describe the run
	// that is in flight, if any.
	cron_pid:     int,
	cron_started: i64,
	// A service the operator deleted keeps a row until its child is reaped.
	removing:     bool,
	// When a PID_FILE points at a master while the process we spawned is still
	// alive (angie), that child is reaped here instead of becoming a zombie.
	orphans:      [dynamic]os.Process,
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
	rotator:      logfile.Rotator,
	last_rotate:  i64, // unix seconds of the last log rotation pass
	last_mem:     i64, // unix seconds of the last RSS sample pass
	cron_sched:   ^cron.Scheduler,
	cron_procs:   map[string]os.Process, // one-shot runs in flight, by service
	removing:     [dynamic]string, // services deleted from the config, awaiting a reap
	cron_bindings: [dynamic]^Cron_Binding,
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
	logfile.rotator_init(&sv.rotator)
	sv.cron_sched = cron.new_scheduler()
	sv.cron_procs = make(map[string]os.Process)
	sv.removing = make([dynamic]string)

	names := config.sorted_service_names(cfg)
	defer delete(names)

	for name in names {
		spec, ok := cfg.services[name]
		if !ok {
			continue
		}
		s := new(Service)
		// The name is cloned, not borrowed, so that every Service owns its key
		// the same way. A borrowed name would make a delete free a slice that
		// belongs to the config map - the allocator rejects that outright.
		s.name = strings.clone(name)
		s.spec = spec
		s.status = .Stopped
		s.exits = make([dynamic]i64)
		s.orphans = make([dynamic]os.Process)
		sv.services[s.name] = s
		append(&sv.order, s.name)
		register_cron(sv, s)
	}
	// The loop walks sv.order, not cfg.topo_order, so the dependency order has
	// to be applied here (and re-applied by every config edit).
	rebuild_order(sv)

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
		if is_scheduled(s) {
			// A CRON service is fired by the scheduler, not started here.
			s.status = .Stopped
			continue
		}
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
	// Reverse dependency order: a consumer stops before the service it uses,
	// the mirror image of start_all walking sv.order forwards.
	for i := len(sv.order) - 1; i >= 0; i -= 1 {
		s, ok := sv.services[sv.order[i]]
		if !ok {
			continue
		}
		s.want_running = false
		if s.pid > 0 {
			s.status = .Stopping
			signal(s.pid, .SIGTERM)
			s.kill_at = now + STOP_GRACE
		}
		if s.cron_pid > 0 {
			s.status = .Stopping
			signal(s.cron_pid, .SIGTERM)
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
	if is_scheduled(s) {
		// "start" on a scheduled service means "run it now"; the timer keeps
		// its own cadence either way.
		result, pid := cron_start_locked(sv, s, now_seconds(sv))
		if len(result) > 0 {
			return fmt.tprintf("cannot start {}: {}", name, result)
		}
		record_run(sv, name, "started", now_seconds(sv), 0, pid, false)
		return ""
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
//
// The status turns Stopping right here rather than when the exit is reaped: the
// caller returns to the UI immediately, and an API read a millisecond later has
// to agree that the service is on its way down instead of still reporting
// `running` for as long as the next poll takes.
stop :: proc(sv: ^Supervisor, name: string) -> string {
	sync.rw_mutex_lock(&sv.mu)
	defer sync.rw_mutex_unlock(&sv.mu)
	s, ok := sv.services[name]
	if !ok {
		return fmt.tprintf("unknown service: {}", name)
	}
	s.want_running = false
	s.restarting = false
	if is_scheduled(s) {
		// Stop the run that is in flight; the next scheduled fire picks up.
		if s.cron_pid > 0 {
			s.status = .Stopping
			signal(s.cron_pid, .SIGTERM)
		}
		return ""
	}
	if s.pid > 0 {
		s.status = .Stopping
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
	if is_scheduled(s) {
		// A scheduled service has no resident process to restart: end the run
		// that is in flight and let the schedule decide when the next one is.
		if s.cron_pid > 0 {
			s.status = .Stopping
			signal(s.cron_pid, .SIGTERM)
		}
		sync.rw_mutex_unlock(&sv.mu)
		return ""
	}
	if s.spec.restart_cmd != "" && s.pid > 0 {
		cmd := s.spec.restart_cmd
		dir := s.spec.work_dir
		env := child_env(s.spec.env_vars)
		sync.rw_mutex_unlock(&sv.mu)
		defer child_env_destroy(&env)
		if !run_shell(cmd, dir, 15 * time.Second, child_env_view(&env)) {
			return fmt.tprintf("reload command failed for {}", name)
		}
		return ""
	}
	s.want_running = true
	s.restarting = true
	if s.pid > 0 {
		// Same reasoning as stop: the old process is still alive for a poll or
		// two, but it is already on its way out, and the card must say so.
		s.status = .Stopping
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
		// A scheduled run in flight is what the dashboard should see, even
		// though the service itself has no resident pid - unless that run is
		// being stopped, which outranks "it is running right now".
		if s.cron_pid > 0 {
			if s.status != .Stopping {
				ss.status = .Running
			}
			ss.pid = s.cron_pid
			if s.cron_started > 0 {
				ss.started_at = s.cron_started
			}
		}
		if s.pid > 0 || s.cron_pid > 0 {
			ss.memory_mb = s.memory_mb
		}
		// Only a live process has an age. Without this, a stopped service kept
		// displaying the uptime it had before it exited, because started_at is
		// retained for state recovery.
		if ss.pid <= 0 || !status.has_process(ss.status) {
			ss.started_at = 0
		}
		if s.exit_code != 0 || s.status == .Crashed || s.status == .Failed {
			// `&code` on a local here is a dangling pointer: snapshot returns
			// before anyone formats the value, and the frame it pointed into is
			// reused by then (the result was exit codes like 2449958201634562824).
			// The rest of the snapshot already lives in the caller's arena, so
			// the exit code goes there too.
			code := new(int)
			code^ = s.exit_code
			ss.exit_code = code
		}
		status.compute_uptime(&ss)
		out[i] = ss
	}
	return out
}

// Mem_Sample pairs a service name with the pid whose tree to measure, so the
// /proc walk can run after the lock is released.
Mem_Sample :: struct {
	name: string,
	pid:  int,
}

// sample_memory refreshes the resident memory of every running service. The
// per-pid reads run outside the lock so a slow walk cannot stall the API;
// only the field write takes the write lock, and a pid that changed in between
// is skipped rather than its memory attributed to the wrong process.
sample_memory :: proc(sv: ^Supervisor) {
	sync.rw_mutex_shared_lock(&sv.mu)
	pending: [dynamic]Mem_Sample
	for name in sv.order {
		s := sv.services[name]
		if s.pid > 0 && (s.status == .Running || s.status == .Starting) {
			append(&pending, Mem_Sample{name, s.pid})
		}
	}
	sync.rw_mutex_shared_unlock(&sv.mu)
	defer delete(pending)

	for p in pending {
		mb := read_rss_mb(p.pid)
		if mb <= 0 {
			continue
		}
		sync.rw_mutex_lock(&sv.mu)
		if s, ok := sv.services[p.name]; ok && s.pid == p.pid {
			s.memory_mb = mb
		}
		sync.rw_mutex_unlock(&sv.mu)
	}
}

// tick is one pass of the loop: reap exits, apply restart deadlines, then start
// whatever is due. It reports whether anything changed, so the caller knows when
// a state snapshot is worth writing.
tick :: proc(sv: ^Supervisor) -> bool {
	sync.rw_mutex_lock(&sv.mu)
	defer sync.rw_mutex_unlock(&sv.mu)
	now := now_seconds(sv)
	changed := reap_cron_locked(sv, now)
	reap_removed_locked(sv, now)
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

// poll_locked reaps an exit and applies the SIGKILL deadline. Not file-private:
// the delete path in service_admin reaps a service being removed through it.
poll_locked :: proc(sv: ^Supervisor, s: ^Service, now: i64) -> bool {
	changed := false
	// A PID_FILE master may be adopted while the process we spawned is still
	// alive (angie); keep reaping that handle so it cannot become a zombie.
	i := 0
	for i < len(s.orphans) {
		st, err := os.process_wait(s.orphans[i], 0)
		if err != nil || st.exited {
			ordered_remove(&s.orphans, i)
		} else {
			i += 1
		}
	}
	if s.pid > 0 {
		if s.owned {
			st, err := os.process_wait(s.handle, 0)
			if err == nil && st.exited {
				// A daemon (nginx/angie) forks a master and lets the process we
				// spawned exit. Adopt the master instead of treating this as a
				// crash; otherwise the restart policy starts a second copy.
				if master := find_master_locked(sv, s); master > 0 {
					adopt_master_locked(sv, s, master)
				} else {
					on_exit_locked(sv, s, now, st.exit_code)
					s.pid = 0
					s.owned = false
				}
				changed = true
			}
		} else if !alive(s.pid) {
			if master := find_master_locked(sv, s); master > 0 {
				adopt_master_locked(sv, s, master)
			} else {
				// Adopted process: no handle, so the exit code is unknown (-1).
				on_exit_locked(sv, s, now, -1)
				s.pid = 0
			}
			changed = true
		}

		// angie may keep the started parent alive while the real master is
		// already recorded in its PID_FILE, so look there during the probe
		// window even before our own child exits.
		if s.pid > 0 && !s.daemon_probe_done && s.spec.pid_file != "" {
			if now >= s.daemon_probe_until {
				s.daemon_probe_done = true
			} else if pid := read_service_pid_file(s.spec); pid > 0 && pid != s.pid &&
				pid != current_pid() && alive(pid) && !pid_tracked_elsewhere(sv, s, pid) {
				adopt_master_locked(sv, s, pid)
				changed = true
			}
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

// pid_tracked_elsewhere keeps a shared executable from being adopted twice:
// two services can run the same binary (a daemon and a cron job), and only the
// one whose child we just lost may claim a new pid.
@(private = "file")
pid_tracked_elsewhere :: proc(sv: ^Supervisor, self: ^Service, pid: int) -> bool {
	for name in sv.order {
		other := sv.services[name]
		if other == self {
			continue
		}
		if other.pid == pid || other.cron_pid == pid {
			return true
		}
	}
	return false
}

// read_service_pid_file resolves a relative PID_FILE against WORK_DIR, which is
// the directory the service itself runs in.
@(private = "file")
read_service_pid_file :: proc(spec: config.ServiceConfig) -> int {
	path := spec.pid_file
	if len(path) == 0 {
		return 0
	}
	if path[0] != '/' && len(spec.work_dir) > 0 {
		joined := fmt.aprintf("{}/{}", spec.work_dir, path)
		defer delete(joined)
		return read_pid_file(joined)
	}
	return read_pid_file(path)
}

// find_master_locked returns the pid a daemonized service should be tracked by:
// the PID_FILE value when the operator configured one, otherwise a process with
// the same executable name that was reparented to init. It is called only while
// the service is supposed to be running, so a manual stop or restart never
// adopts the process it is about to kill.
@(private = "file")
find_master_locked :: proc(sv: ^Supervisor, s: ^Service) -> int {
	if !s.want_running || s.restarting || s.removing || s.status == .Failed {
		return 0
	}
	self := current_pid()
	if pid := read_service_pid_file(s.spec); pid > 0 && pid != s.pid && pid != self &&
		alive(pid) && !pid_tracked_elsewhere(sv, s, pid) {
		return pid
	}
	exclude := make([dynamic]int, 0, len(sv.order))
	defer delete(exclude)
	for name in sv.order {
		other := sv.services[name]
		if other == s {
			continue
		}
		if other.pid > 0 {
			append(&exclude, other.pid)
		}
		if other.cron_pid > 0 {
			append(&exclude, other.cron_pid)
		}
	}
	pid := find_main_process(s.spec.exec_cmd, exclude[:])
	if pid > 0 && pid != s.pid && pid != self && alive(pid) &&
		!pid_tracked_elsewhere(sv, s, pid) && master_started_near(pid, s.started_at) {
		return pid
	}
	return 0
}

// adopt_master_locked switches tracking to the daemon master. started_at is
// kept: it is when the service was launched, which is what uptime should show.
@(private = "file")
adopt_master_locked :: proc(sv: ^Supervisor, s: ^Service, pid: int) {
	old := s.pid
	if s.owned && alive(old) {
		append(&s.orphans, s.handle)
	}
	s.pid = pid
	s.owned = false
	s.handle = {}
	s.status = .Running
	s.kill_at = 0
	s.daemon_probe_done = true
	glog.info("adopted daemon master", "service", s.name, "old_pid", old, "pid", pid)
	persist_locked(sv, s)
}

@(private = "file")
on_exit_locked :: proc(sv: ^Supervisor, s: ^Service, now: i64, exit_code: int) {
	s.exited_at = now
	s.exit_code = exit_code
	s.kill_at = 0
	s.memory_mb = 0 // the tree it measured is gone; a restart re-samples

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
	env := child_env(s.spec.env_vars)
	defer child_env_destroy(&env)
	if s.spec.pre_action != "" {
		if !run_shell(s.spec.pre_action, s.spec.work_dir, 15 * time.Second, child_env_view(&env)) {
			glog.warn("PRE_ACTION failed, starting anyway", "service", s.name)
		}
	}
	// CHECK_PORT clears a stale listener before the child tries to bind it.
	clear_check_port(sv, s)

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
		env         = child_env_view(&env),
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
	s.daemon_probe_until = now + DAEMON_PROBE
	s.daemon_probe_done = false
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
	if sv.cron_sched != nil {
		cron.stop(sv.cron_sched)
	}
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
		delete(s.orphans)
		delete(s.name)
		free(s)
	}
	delete(sv.services)
	delete(sv.order)
	delete(sv.removing)
	logfile.rotator_destroy(&sv.rotator)
	if sv.cron_sched != nil {
		cron.destroy_scheduler(sv.cron_sched)
		sv.cron_sched = nil
	}
	delete(sv.cron_procs)
	for b in sv.cron_bindings {
		free(b)
	}
	delete(sv.cron_bindings)
	sv.services = nil
	sv.order = nil
	sv.cfg = nil
}

// ---- logs ----

// log_path resolves the file a service writes to. `kind` is "stdout" or
// "stderr"; when only one of the two is configured the other falls back to it,
// because that is the file the operator will want to read either way.
log_path :: proc(sv: ^Supervisor, name, kind: string) -> (string, bool) {
	sync.rw_mutex_shared_lock(&sv.mu)
	defer sync.rw_mutex_shared_unlock(&sv.mu)
	s, ok := sv.services[name]
	if !ok {
		return "", false
	}
	path := s.spec.stdout
	if kind == "stderr" {
		path = s.spec.stderr
	}
	if len(path) == 0 {
		path = s.spec.stderr
	}
	if len(path) == 0 {
		path = s.spec.stdout
	}
	return path, true
}

// read_logs returns the last `lines` lines of a service log. The caller owns the
// tail and must pass it to logfile.tail_destroy.
read_logs :: proc(sv: ^Supervisor, name, kind: string, lines: int) -> (logfile.Tail, string, string) {
	path, ok := log_path(sv, name, kind)
	if !ok {
		return {}, "", fmt.tprintf("unknown service: {}", name)
	}
	if len(path) == 0 {
		// Nothing configured and nothing written: an empty log, not an error.
		return {}, "", ""
	}
	t, err := logfile.tail(path, lines)
	return t, path, err
}

clear_logs :: proc(sv: ^Supervisor, name, kind: string) -> string {
	path, ok := log_path(sv, name, kind)
	if !ok {
		return fmt.tprintf("unknown service: {}", name)
	}
	return logfile.clear(path)
}

// rotate_logs gives every configured log file a new day's file. It runs once at
// startup (to catch up on days gorch was not running) and then once a minute.
@(private = "file")
rotate_logs :: proc(sv: ^Supervisor, now: time.Time) {
	paths := make([dynamic]string, 0, len(sv.order) * 2)
	defer delete(paths)

	sync.rw_mutex_shared_lock(&sv.mu)
	for name in sv.order {
		s := sv.services[name]
		if len(s.spec.stdout) > 0 {
			append(&paths, s.spec.stdout)
		}
		// stdout and stderr often point at the same file; rotating it twice
		// would just be a no-op, but skipping the duplicate keeps the log quiet.
		if len(s.spec.stderr) > 0 && s.spec.stderr != s.spec.stdout {
			append(&paths, s.spec.stderr)
		}
	}
	sync.rw_mutex_shared_unlock(&sv.mu)

	for path in paths {
		moved, err := logfile.rotate(&sv.rotator, path, now)
		if len(err) > 0 {
			glog.warn("cannot rotate log", "path", path, "err", err)
		} else if len(moved) > 0 {
			glog.info("rotated log", "path", path, "to", moved)
		}
		delete(moved)
	}
}

// Loop runs the supervisor until halt is set. Meant to be the body of a thread.
Loop :: struct {
	sv:     ^Supervisor,
	halted: u32,
}

// spawn_loop starts the tick loop on its own thread, along with the cron
// scheduler that fires the scheduled services.
spawn_loop :: proc(sv: ^Supervisor) -> ^Loop {
	l := new(Loop)
	l.sv = sv
	cron.start(sv.cron_sched)
	if !worker.spawn(loop_entry, rawptr(l)) {
		glog.error("cannot start the supervisor loop thread")
		sync.atomic_store(&l.halted, 1)
	}
	return l
}

// loop_entry is the pthread body; `l` lives as long as the process does.
loop_entry :: proc "c" (arg: rawptr) -> rawptr {
	context = runtime.default_context()
	loop_run((^Loop)(arg))
	worker.leave()
	return nil
}

// halt_loop asks the loop to return; the caller still owns the Supervisor.
halt_loop :: proc(l: ^Loop) {
	if l != nil {
		sync.atomic_store(&l.halted, 1)
	}
}

loop_run :: proc(l: ^Loop) {
	// Catch up first: a file last written to before gorch was restarted still
	// belongs to the day it was written on.
	rotate_logs(l.sv, time.now())
	l.sv.last_rotate = now_seconds(l.sv)

	for sync.atomic_load(&l.halted) == 0 {
		changed := tick(l.sv)
		if changed {
			_ = flush(l.sv)
		} else if l.sv.has_store {
			_ = state.save_if_due(&l.sv.store, SAVE_EVERY)
		}
		if now := now_seconds(l.sv); now-l.sv.last_mem >= MEM_EVERY {
			sample_memory(l.sv)
			l.sv.last_mem = now
		}
		if now := now_seconds(l.sv); now-l.sv.last_rotate >= ROTATE_EVERY {
			rotate_logs(l.sv, time.now())
			l.sv.last_rotate = now
		}
		time.sleep(POLL)
	}
}
