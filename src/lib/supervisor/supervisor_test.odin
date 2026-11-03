package supervisor

import "core:os"
import "core:testing"
import "core:time"

import "../../lib/config"
import "../../lib/logfile"
import "../../lib/status"

// Tests drive real processes but only harmless ones (`sleep`, `exit 1`), and each
// case owns its own state file and service names: odin runs cases on parallel
// threads, so shared globals would race.

@(test)
test_proc_start_time :: proc(t: ^testing.T) {
	p, err := os.process_start(os.Process_Desc{command = []string{"/bin/sh", "-c", "sleep 5"}})
	testing.expect(t, err == nil, "helper must start")
	defer _ = os.process_kill(p)

	time.sleep(150 * time.Millisecond)
	now := time.to_unix_seconds(time.now())
	start, ok := proc_start_time(p.pid)
	testing.expect(t, ok, "the kernel must report a start time")
	testing.expectf(t, start <= now && start >= now - 5, "start {} now {}", start, now)

	_, bad := proc_start_time(-1)
	testing.expect(t, !bad, "invalid pid must fail")
}

@(test)
test_policy_of :: proc(t: ^testing.T) {
	testing.expect(t, policy_of("always") == .Always)
	testing.expect(t, policy_of("on-failure") == .On_Failure)
	testing.expect(t, policy_of("never") == .Never)
	testing.expect(t, policy_of("") == .Never)
	testing.expect(t, policy_of("garbage") == .Never)
}

@(private = "file")
wait_for :: proc(sv: ^Supervisor, name: string, want: status.Status, ticks: int) -> bool {
	for _ in 0 ..< ticks {
		tick(sv)
		if s, ok := sv.services[name]; ok && s.status == want {
			return true
		}
		time.sleep(20 * time.Millisecond)
	}
	return false
}

// A test config is built by hand, so it must be torn down by hand too; the leak
// tracker in `odin test` otherwise blames the code under test.
@(private = "file")
new_test_config :: proc(name, exec_cmd, policy: string) -> ^config.Config {
	cfg := new(config.Config)
	cfg.services = make(map[string]config.ServiceConfig)
	cfg.services[name] = config.ServiceConfig {
		exec_cmd       = exec_cmd,
		work_dir       = "/tmp",
		restart_policy = policy,
	}
	return cfg
}

@(private = "file")
free_test_config :: proc(cfg: ^config.Config) {
	delete(cfg.services)
	free(cfg)
}

@(test)
test_start_stop_lifecycle :: proc(t: ^testing.T) {
	cfg := new_test_config("t_sleeper", "sleep 60", "never")
	defer free_test_config(cfg)

	sv := new(Supervisor)
	defer free(sv)
	init(sv, cfg, "/tmp/gorch_sup_lifecycle.state")
	defer {
		stop_all(sv)
		for _ in 0 ..< 40 {
			if !tick(sv) {
				break
			}
			time.sleep(20 * time.Millisecond)
		}
		shutdown(sv)
		destroy(sv)
	}

	start_all(sv)
	testing.expect(t, wait_for(sv, "t_sleeper", .Running, 50), "service should reach Running")

	s := sv.services["t_sleeper"]
	testing.expectf(t, s.pid > 0, "pid {}", s.pid)
	testing.expect(t, s.owned, "we spawned it, so we must reap it")
	pid := s.pid

	testing.expect(t, stop(sv, "t_sleeper") == "")
	testing.expect(t, wait_for(sv, "t_sleeper", .Stopped, 100), "service should stop")
	testing.expect(t, s.pid == 0, "pid must be cleared after reaping")
	testing.expect(t, !alive(pid), "process must be gone")

	testing.expect(t, stop(sv, "nope") != "", "unknown service must error")
}

@(test)
test_restart_budget_escalates :: proc(t: ^testing.T) {
	// exits 1 immediately, so every restart burns budget
	cfg := new_test_config("t_crasher", "/bin/sh -c 'exit 1'", "on-failure")
	defer free_test_config(cfg)

	sv := new(Supervisor)
	defer free(sv)
	init(sv, cfg, "/tmp/gorch_sup_budget.state")
	defer {
		shutdown(sv)
		destroy(sv)
	}

	sv.max_restarts = 2
	sv.default_back_off = 0 // no wait between attempts
	start_all(sv)

	testing.expect(t, wait_for(sv, "t_crasher", .Failed, 200), "budget must escalate to Failed")
	s := sv.services["t_crasher"]
	testing.expectf(t, s.restarts > sv.max_restarts, "restarts {} max {}", s.restarts, sv.max_restarts)
	testing.expect(t, s.pid == 0, "escalated service must not hold a pid")
	testing.expect(t, !s.want_running, "escalated service must stay down")

	// a manual start clears the escalation
	testing.expect(t, start(sv, "t_crasher") == "")
	testing.expect(t, s.status != .Failed, "manual start clears Failed")
	stop_all(sv)
	for _ in 0 ..< 40 {
		if !tick(sv) {
			break
		}
		time.sleep(20 * time.Millisecond)
	}
}

@(test)
test_log_path_and_tail :: proc(t: ^testing.T) {
	dir := "/tmp/gorch_sup_logs"
	_ = os.make_directory(dir)
	defer _ = os.remove_all(dir)
	_ = os.write_entire_file("/tmp/gorch_sup_logs/svc.out.log", "alpha\nbeta\n")

	cfg := new(config.Config)
	defer free_test_config(cfg)
	cfg.services = make(map[string]config.ServiceConfig)
	cfg.services["t_logs"] = config.ServiceConfig {
		exec_cmd = "sleep 1",
		work_dir = "/tmp",
		stdout   = "/tmp/gorch_sup_logs/svc.out.log",
	}

	sv := new(Supervisor)
	defer free(sv)
	init(sv, cfg, "")
	defer destroy(sv)

	path, ok := log_path(sv, "t_logs", "stdout")
	testing.expect(t, ok, "configured service must resolve")
	testing.expect_value(t, path, "/tmp/gorch_sup_logs/svc.out.log")

	// No STDERR configured: the operator still wants to read the only file
	// there is, so stderr falls back to stdout rather than to nothing.
	fallback, _ := log_path(sv, "t_logs", "stderr")
	testing.expect_value(t, fallback, "/tmp/gorch_sup_logs/svc.out.log")

	_, unknown := log_path(sv, "nope", "stdout")
	testing.expect(t, !unknown, "unknown service must not resolve")

	tail, tail_path, err := read_logs(sv, "t_logs", "stdout", 10)
	defer logfile.tail_destroy(&tail)
	testing.expectf(t, err == "", "read_logs failed: {}", err)
	testing.expect_value(t, tail_path, "/tmp/gorch_sup_logs/svc.out.log")
	testing.expectf(t, len(tail.lines) == 2, "got {} lines", len(tail.lines))

	testing.expect_value(t, clear_logs(sv, "t_logs", "stdout"), "")
	empty, _, _ := read_logs(sv, "t_logs", "stdout", 10)
	defer logfile.tail_destroy(&empty)
	testing.expect_value(t, len(empty.lines), 0)
}

@(test)
test_recover_adopts_running_child :: proc(t: ^testing.T) {
	path := "/tmp/gorch_sup_recover.state"

	cfg := new_test_config("t_adopt", "sleep 30", "never")
	defer free_test_config(cfg)

	first := new(Supervisor)
	init(first, cfg, path)
	start_all(first)
	testing.expect(t, wait_for(first, "t_adopt", .Running, 50))
	pid := first.services["t_adopt"].pid
	testing.expect(t, pid > 0)

	// Simulate a gorch restart: a second supervisor reads the same store. Our
	// child is still a child of this test process, so the first supervisor keeps
	// the handle and does the reaping at the end (in production the old process is
	// gone and launchd reparents the child).
	second := new(Supervisor)
	init(second, cfg, path)
	adopted := second.services["t_adopt"]
	testing.expectf(t, adopted.pid == pid, "adopted pid {} want {}", adopted.pid, pid)
	testing.expect(t, adopted.status == .Running, "adopted service reports Running")
	testing.expect(t, adopted.want_running, "adopted service stays wanted")
	testing.expect(t, !adopted.owned, "adopted processes have no wait handle")

	// Cleanup: kill through the owner so the exit is actually reaped. Keep ticking
	// for a fixed number of rounds: the first pass may happen before the signal
	// lands, and `tick` reports "nothing changed" while the child is still dying.
	signal(pid, .SIGKILL)
	for _ in 0 ..< 50 {
		_ = tick(first)
		if !alive(pid) {
			break
		}
		time.sleep(20 * time.Millisecond)
	}
	testing.expect(t, !alive(pid), "child must be gone after cleanup")

	shutdown(first)
	shutdown(second)
	destroy(first)
	destroy(second)
	free(first)
	free(second)
}
