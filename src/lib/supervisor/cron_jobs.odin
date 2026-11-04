package supervisor

// Scheduled services.
//
// A service with a CRON expression is not a daemon: gorch does not start it at
// boot and keeps no resident pid for it. The scheduler fires EXEC_CMD on the
// expression, each run is a one-shot child that is reaped when it exits, and the
// last few runs are kept as a history the dashboard shows.
//
// Overlap is decided from the process table, not from the scheduler: the firing
// callback returns as soon as the child is spawned, so the scheduler's own
// "still running" flag clears immediately and cannot tell a long job from a slow
// one.

import "core:fmt"
import "core:os"
import "core:sync"
import "core:time"

import "../../lib/config"
import "../../lib/cron"
import glog "../../lib/log"

// Cron_Binding is the per-job context handed to the scheduler. It is a heap
// allocation because the scheduler keeps the pointer for the life of the job.
Cron_Binding :: struct {
	sv:   ^Supervisor,
	name: string,
}

// register_cron turns every service that has a CRON expression into a job.
// Editing a schedule goes through here again, so the binding is reused instead
// of growing the list by one per save.
register_cron :: proc(sv: ^Supervisor, s: ^Service) {
	if len(s.spec.cron) == 0 {
		return
	}
	b := binding_of(sv, s.name)
	if b == nil {
		b = new(Cron_Binding)
		b.sv = sv
		b.name = s.name
		append(&sv.cron_bindings, b)
	}
	if !cron.add_job(sv.cron_sched, s.name, s.spec.cron, "", cron_fire, b) {
		glog.error("invalid CRON expression, service will not run", "service", s.name, "cron", s.spec.cron)
	}
}

// unbind_cron drops the job so a schedule change does not leave the old
// expression firing.
unbind_cron :: proc(sv: ^Supervisor, name: string) {
	if sv.cron_sched != nil {
		_ = cron.remove_job(sv.cron_sched, name)
	}
}

binding_of :: proc(sv: ^Supervisor, name: string) -> ^Cron_Binding {
	for b in sv.cron_bindings {
		if b.name == name {
			return b
		}
	}
	return nil
}

// is_scheduled reports whether a service runs on a timer rather than as a daemon.
is_scheduled :: proc(s: ^Service) -> bool {
	return len(s.spec.cron) > 0
}

cron_fire :: proc(user: rawptr) {
	b := cast(^Cron_Binding)user
	sv := b.sv
	now := now_seconds(sv)

	sync.rw_mutex_lock(&sv.mu)
	s, ok := sv.services[b.name]
	result := ""
	pid := 0
	if ok {
		result, pid = cron_start_locked(sv, s, now)
	}
	sync.rw_mutex_unlock(&sv.mu)

	if !ok {
		return
	}
	switch result {
	case "":
		record_run(sv, b.name, "started", now, 0, pid, false)
		glog.info("cron run started", "service", b.name, "pid", pid)
	case "overlap":
		record_run(sv, b.name, "overlap", now, 0, 0, false)
		glog.warn("previous cron run still going, skipping this one", "service", b.name)
	case:
		record_run(sv, b.name, "failed", now, 0, 0, false)
		glog.error("cannot start cron run", "service", b.name, "err", result)
	}
}

// cron_start_locked spawns one run. Returns an empty string on success and the
// new pid, or a reason the run did not happen.
cron_start_locked :: proc(sv: ^Supervisor, s: ^Service, now: i64) -> (string, int) {
	if p, running := sv.cron_procs[s.name]; running && alive(p.pid) {
		return "overlap", 0
	}
	if len(s.spec.exec_cmd) == 0 {
		return "service has no EXEC_CMD", 0
	}

	stdout_file, stderr_file: ^os.File
	if len(s.spec.stdout) > 0 {
		if f, err := os.open(s.spec.stdout, {.Write, .Create, .Append}); err == nil {
			stdout_file = f
		}
	}
	if len(s.spec.stderr) > 0 {
		if f, err := os.open(s.spec.stderr, {.Write, .Create, .Append}); err == nil {
			stderr_file = f
		}
	}

	cmd_buf: [COMMAND_MAX]u8
	command, cmd_ok := wrap_command(cmd_buf[:], s.spec.exec_cmd)
	if !cmd_ok {
		if stdout_file != nil {
			os.close(stdout_file)
		}
		if stderr_file != nil {
			os.close(stderr_file)
		}
		return "EXEC_CMD is too long", 0
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
		return fmt.tprintf("cannot start: {}", err), 0
	}

	sv.cron_procs[s.name] = p
	s.cron_pid = p.pid
	s.cron_started = now
	return "", p.pid
}

// reap_cron_locked collects finished runs and kills ones that outlived
// CRON_TIMEOUT. It reports whether anything changed.
reap_cron_locked :: proc(sv: ^Supervisor, now: i64) -> bool {
	if len(sv.cron_procs) == 0 {
		return false
	}
	changed := false

	// Deleting from a map while ranging over it is not allowed, so the names
	// come first.
	finished := make([dynamic]string)
	defer delete(finished)

	for name, handle in sv.cron_procs {
		s, ok := sv.services[name]
		if !ok {
			append(&finished, name)
			continue
		}
		timeout := s.spec.cron_timeout
		if timeout > 0 && s.cron_started > 0 && now-s.cron_started > i64(timeout) {
			signal(s.cron_pid, .SIGKILL)
			glog.warn("cron run exceeded CRON_TIMEOUT, killed", "service", name, "timeout_s", timeout)
		}

		st, err := os.process_wait(handle, 0)
		if err == nil && st.exited {
			s.cron_pid = 0
			s.cron_started = 0
			status_text := st.exit_code == 0 ? "success" : "failed"
			record_run(sv, name, status_text, now, st.exit_code, 0, true)
			append(&finished, name)
			changed = true
		}
	}

	for name in finished {
		delete_key(&sv.cron_procs, name)
	}
	return changed
}

// record_run appends one entry to the service's cron history. `record_execution`
// takes ownership of the strings.
record_run :: proc(sv: ^Supervisor, name, status_text: string, now: i64, exit_code, pid: int, finished: bool) {
	rec := cron.CronExecutionRecord {
		service    = fmt.aprintf("{}", name),
		started_at = now,
		status     = fmt.aprintf("{}", status_text),
		pid        = pid,
	}
	if finished {
		ended := new(i64)
		ended^ = now
		rec.ended_at = ended
		code := new(int)
		code^ = exit_code
		rec.exit_code = code
	}
	cron.record_execution(sv.cron_sched, name, rec)
}

// ---- queries used by the web layer ----

cron_history :: proc(sv: ^Supervisor, name: string) -> []cron.CronExecutionRecord {
	if sv.cron_sched == nil {
		return nil
	}
	return cron.get_history(sv.cron_sched, name)
}

cron_next :: proc(sv: ^Supervisor, name: string) -> i64 {
	if sv.cron_sched == nil {
		return -1
	}
	return cron.job_next(sv.cron_sched, name)
}

// cron_validate compiles an expression without touching the scheduler, and
// previews when it would fire so the config page can show it before the save.
// `next` holds unix seconds and is owned by the caller.
Cron_Check :: struct {
	valid:   bool,
	message: string,
	next:    []i64, // up to two firing times
}

cron_validate :: proc(expression: string) -> Cron_Check {
	if len(expression) == 0 {
		return Cron_Check{valid = false, message = "expression is empty"}
	}
	if _, ok := cron.parse(expression); !ok {
		return Cron_Check{valid = false, message = "not a valid cron expression"}
	}
	return Cron_Check{valid = true, next = cron.preview_next(expression, 2)}
}
