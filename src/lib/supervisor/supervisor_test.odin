package supervisor

import "core:net"
import "core:os"
import "core:strings"
import "core:sys/posix"
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

@(test)
test_read_rss_reports_the_running_process :: proc(t: ^testing.T) {
	mb := read_rss_mb(int(posix.getpid()))
	testing.expectf(t, mb > 0, "the test process must own resident memory, got {}", mb)
	testing.expectf(t, mb < 2048, "test process RSS absurdly large: {} MB", mb)

	testing.expect(t, read_rss_mb(-1) == 0, "invalid pid must report unknown")
	testing.expect(t, read_rss_mb(0) == 0, "pid 0 must report unknown")
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
	delete(cfg.topo_order)
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
	// The action returns before the child is reaped, so the status has to say
	// "on its way down" now - the web API reads this field straight after the
	// POST, and answering `running` made Stop look like it did nothing.
	testing.expect_value(t, s.status, status.Status.Stopping)
	testing.expect(t, s.pid == pid, "a stopping process keeps its pid until it exits")
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
test_cron_service_runs_on_schedule_not_at_boot :: proc(t: ^testing.T) {
	dir := "/tmp/gorch_sup_cron"
	_ = os.make_directory(dir)
	out := "/tmp/gorch_sup_cron/run.log"
	_ = os.remove(out)
	defer _ = os.remove_all(dir)

	cfg := new(config.Config)
	defer free_test_config(cfg)
	cfg.services = make(map[string]config.ServiceConfig)
	cfg.services["t_job"] = config.ServiceConfig {
		exec_cmd = "echo ran >> /tmp/gorch_sup_cron/run.log",
		work_dir = "/tmp",
		cron     = "@every 1h",
	}

	sv := new(Supervisor)
	defer free(sv)
	init(sv, cfg, "")
	defer {
		shutdown(sv)
		destroy(sv)
	}

	testing.expect(t, cron_next(sv, "t_job") > 0, "the job must be registered")

	// A scheduled service is not a daemon: booting must not run it.
	start_all(sv)
	s := sv.services["t_job"]
	testing.expect_value(t, s.pid, 0)
	testing.expect_value(t, s.status, status.Status.Stopped)
	testing.expect(t, !os.exists(out), "boot must not run a scheduled service")

	// "start" means "run it now" for these.
	testing.expect_value(t, start(sv, "t_job"), "")
	for _ in 0 ..< 100 {
		_ = tick(sv)
		if !os.exists(out) {
			time.sleep(20 * time.Millisecond)
			continue
		}
		break
	}
	testing.expect(t, os.exists(out), "the run must write its output")
	testing.expect_value(t, s.cron_pid, 0) // reaped by tick

	h := cron_history(sv, "t_job")
	defer delete(h)
	testing.expectf(t, len(h) >= 1, "history has {} records", len(h))
	if len(h) > 0 {
		testing.expect_value(t, h[0].status, "started")
	}
}

@(test)
test_cron_overlap_is_skipped :: proc(t: ^testing.T) {
	cfg := new(config.Config)
	defer free_test_config(cfg)
	cfg.services = make(map[string]config.ServiceConfig)
	cfg.services["t_slow"] = config.ServiceConfig {
		exec_cmd = "sleep 5",
		work_dir = "/tmp",
		cron     = "@every 1h",
	}

	sv := new(Supervisor)
	defer free(sv)
	init(sv, cfg, "")
	defer {
		shutdown(sv)
		destroy(sv)
	}

	testing.expect_value(t, start(sv, "t_slow"), "")
	s := sv.services["t_slow"]
	testing.expect(t, s.cron_pid > 0, "first run must be in flight")

	// A second trigger while the first run is still going is refused instead of
	// stacking another copy of the job on top of it.
	err := start(sv, "t_slow")
	testing.expect(t, len(err) > 0, "overlapping run must be refused")
	testing.expect(t, len(err) > 0 && strings.contains(err, "overlap"))

	stop(sv, "t_slow")
	for _ in 0 ..< 50 {
		if tick(sv) {
			break
		}
		time.sleep(20 * time.Millisecond)
	}
	testing.expect_value(t, s.cron_pid, 0)
}

@(test)
test_cron_validate :: proc(t: ^testing.T) {
	ok := cron_validate("0 */5 * * * *")
	defer delete(ok.next)
	testing.expect(t, ok.valid, ok.message)
	testing.expect(t, len(ok.next) == 2, "a valid expression previews two firings")
	bad := cron_validate("not a cron")
	testing.expect(t, !bad.valid)
	testing.expect(t, len(bad.message) > 0)
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

@(test)
test_add_and_remove_service :: proc(t: ^testing.T) {
	cfg := new(config.Config)
	cfg.services = make(map[string]config.ServiceConfig)
	cfg.services["keep"] = config.ServiceConfig {
		exec_cmd       = "sleep 60",
		work_dir       = "/tmp",
		restart_policy = "never",
	}

	sv := new(Supervisor)
	init(sv, cfg, "")
	defer {
		shutdown(sv)
		destroy(sv)
		free(sv)
		delete(cfg.topo_order)
		delete(cfg.services)
		free(cfg)
	}

	spec := config.ServiceConfig {
		exec_cmd       = "sleep 60",
		work_dir       = "/tmp",
		restart_policy = "never",
	}
	err := add_service(sv, "added", spec)
	testing.expectf(t, err == "", "add_service: {}", err)
	testing.expect(t, len(sv.services) == 2, "service table grew")
	testing.expect(t, sv.services["added"] != nil, "lookup after add")
	for _ in 0 ..< 3 {
		_ = tick(sv)
	}

	edited := spec
	edited.back_off = 7
	edited.depends_on = []string{"keep"}
	testing.expectf(t, set_spec(sv, "added", edited) == "", "set_spec")
	testing.expect(t, sv.services["added"].spec.back_off == 7, "spec replaced")

	testing.expectf(t, remove_service(sv, "added") == "", "remove_service")
	for _ in 0 ..< 10 {
		_ = tick(sv)
	}
	_, still := sv.services["added"]
	testing.expect(t, !still, "removed service leaves the table")
}

// ---- ENV_VARS ----

@(test)
test_child_env_overlays_without_losing_parent :: proc(t: ^testing.T) {
	vars := make(map[string]string)
	defer delete(vars)
	vars["GORCH_TEST_OVERLAY"] = "from-config"
	vars["PATH"] = "/custom/bin"

	env := child_env(vars)
	defer child_env_destroy(&env)
	view := child_env_view(&env)
	testing.expect(t, view != nil, "a non-empty ENV_VARS must produce an env")

	overlay_count, path_count := 0, 0
	for entry in view {
		switch env_key(entry) {
		case "GORCH_TEST_OVERLAY":
			overlay_count += 1
			testing.expect_value(t, entry, "GORCH_TEST_OVERLAY=from-config")
		case "PATH":
			path_count += 1
			testing.expect_value(t, entry, "PATH=/custom/bin")
		}
	}
	testing.expect_value(t, overlay_count, 1)
	// getenv() returns the *first* match, so appending a duplicate would
	// silently keep the parent's value. The overridden key must appear once.
	testing.expect_value(t, path_count, 1)

	home, has_home := os.lookup_env_alloc("HOME", context.allocator)
	if has_home {
		delete(home)
		found := false
		for entry in view {
			if env_key(entry) == "HOME" {
				found = true
			}
		}
		testing.expect(t, found, "unrelated inherited variables must survive")
	}

	empty := child_env(map[string]string{})
	defer child_env_destroy(&empty)
	testing.expect(t, child_env_view(&empty) == nil, "no ENV_VARS means inherit")
}

@(test)
test_env_vars_reaches_the_child :: proc(t: ^testing.T) {
	dir := "/tmp/gorch_sup_env"
	_ = os.make_directory(dir)
	defer _ = os.remove_all(dir)

	cfg := new(config.Config)
	defer free_test_config(cfg)
	cfg.services = make(map[string]config.ServiceConfig)
	vars := make(map[string]string)
	defer delete(vars)
	vars["GORCH_TEST_VAR"] = "hello-gorch"
	cfg.services["t_env"] = config.ServiceConfig {
		exec_cmd       = "printenv GORCH_TEST_VAR > /tmp/gorch_sup_env/value.txt",
		work_dir       = "/tmp",
		restart_policy = "never",
		env_vars       = vars,
	}

	sv := new(Supervisor)
	defer free(sv)
	init(sv, cfg, "")
	defer {
		shutdown(sv)
		destroy(sv)
	}

	start_all(sv)
	for _ in 0 ..< 100 {
		if os.exists("/tmp/gorch_sup_env/value.txt") {
			break
		}
		time.sleep(20 * time.Millisecond)
	}
	data, err := os.read_entire_file_from_path("/tmp/gorch_sup_env/value.txt", context.allocator)
	testing.expectf(t, err == nil, "the child must write its env: {}", err)
	if err == nil {
		testing.expect_value(t, strings.trim_space(string(data)), "hello-gorch")
		delete(data)
	}
}

// ---- DEPENDS_ON ----

@(test)
test_depends_on_orders_the_loop :: proc(t: ^testing.T) {
	cfg := new(config.Config)
	defer free_test_config(cfg)
	cfg.services = make(map[string]config.ServiceConfig)
	// Alphabetically a_api comes first, but it depends on z_db: an alphabetical
	// sv.order would start the consumer before its dependency.
	cfg.services["a_api"] = config.ServiceConfig {
		exec_cmd   = "sleep 60",
		work_dir   = "/tmp",
		depends_on = []string{"z_db"},
	}
	cfg.services["z_db"] = config.ServiceConfig {
		exec_cmd = "sleep 60",
		work_dir = "/tmp",
	}

	sv := new(Supervisor)
	defer free(sv)
	init(sv, cfg, "")
	defer destroy(sv)

	testing.expect_value(t, len(sv.order), 2)
	testing.expect_value(t, sv.order[0], "z_db")
	testing.expect_value(t, sv.order[1], "a_api")
	testing.expect_value(t, sv.cfg.topo_order[0], "z_db")

	// Removing the dependency must re-sort the runtime table, not just cfg.
	edited := cfg.services["a_api"]
	edited.depends_on = nil
	testing.expect_value(t, set_spec(sv, "a_api", edited), "")
	testing.expect_value(t, sv.order[0], "a_api")
	testing.expect_value(t, sv.cfg.topo_order[0], "a_api")
}

// ---- CHECK_PORT ----

@(test)
test_port_detection_finds_the_listener_owner :: proc(t: ^testing.T) {
	port :: 18999
	listener, err := net.listen_tcp(net.Endpoint{net.IP4_Loopback, port})
	if err != nil {
		return // port taken by an unrelated process on this machine
	}
	testing.expect(t, port_is_busy(port), "a live listener must be detected")

	owners := port_owners(port)
	defer delete(owners)
	self := int(posix.getpid())
	found := false
	for pid in owners {
		if pid == self {
			found = true
		}
	}
	testing.expectf(t, found, "port_owners must find the test process, got {}", owners)

	// Do not assert that the port is free after close: odin runs tests in
	// parallel and another case may have spawned a child while this listener
	// was open, inheriting its fd and keeping the socket alive. That is a
	// property of the test runner, not of the pre-flight check.
	net.close(listener)
}
