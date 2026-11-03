package supervisor

// Mutations driven by the config page.
//
// The web layer owns JSON; this file owns the service table. Everything here
// takes the supervisor lock, because the loop thread reads `spec` while it
// spawns and reaps, and a half-applied spec would leave a service running with
// one command and restarting with another.
//
// Each write also switches `context` to the default allocator first: requests
// run on a connection thread whose allocator is an arena that dies with the
// response, so a Service (or its name) kept in the table has to come from the
// heap. That switch cannot be factored into a helper - Odin hands every
// procedure its own copy of `context`, so an assignment inside a callee never
// reaches the caller.

import "base:runtime"
import "core:fmt"
import "core:strings"
import "core:sync"

import "../../lib/config"

// validate_spec rejects what the daemon could not honour anyway. It runs before
// anything is written so a bad edit never reaches the table.
validate_spec :: proc(cfg: ^config.Config, name: string, spec: config.ServiceConfig) -> string {
	if len(name) == 0 {
		return "service name is required"
	}
	if len(spec.exec_cmd) == 0 && len(spec.cron) == 0 {
		return "EXEC_CMD is required unless CRON is set"
	}
	if !config.is_valid_restart_policy(spec.restart_policy) {
		return fmt.tprintf("invalid RESTART_POLICY: {}", spec.restart_policy)
	}
	if spec.back_off < 0 || spec.check_port < 0 || spec.cron_timeout < 0 {
		return "BACK_OFF, CHECK_PORT and CRON_TIMEOUT must not be negative"
	}
	if spec.check_port > 65535 {
		return "CHECK_PORT must be a TCP port"
	}
	for dep in spec.depends_on {
		if dep == name {
			return fmt.tprintf("service {} depends on itself", name)
		}
		if _, ok := cfg.services[dep]; !ok {
			return fmt.tprintf("unknown dependency: {}", dep)
		}
	}
	return ""
}

// rebuild_order recomputes the dependency order after the service map changed.
// A cycle is rejected by the caller beforehand, so this only has to store the
// result; on the impossible error the name order is kept.
rebuild_order :: proc(sv: ^Supervisor) {
	order, err := config.topological_sort(sv.cfg)
	if err == "" {
		sv.cfg.topo_order = order
	}
}

// set_spec applies an edited config to a service that already exists. Fields
// that only matter at spawn time (command, env, back-off) take effect on the
// next start; a changed CRON expression is rebound to the scheduler at once.
set_spec :: proc(sv: ^Supervisor, name: string, spec: config.ServiceConfig) -> string {
	prev_ctx := context
	context = runtime.default_context()
	defer context = prev_ctx
	sync.rw_mutex_lock(&sv.mu)
	defer sync.rw_mutex_unlock(&sv.mu)

	s, ok := sv.services[name]
	if !ok {
		return fmt.tprintf("unknown service: {}", name)
	}
	if err := validate_spec(sv.cfg, name, spec); len(err) > 0 {
		return err
	}
	if e := config_cycle_check(sv.cfg, name, spec); len(e) > 0 {
		return e
	}

	old := s.spec
	s.spec = spec
	sv.cfg.services[name] = spec
	rebuild_order(sv)

	if old.cron != spec.cron {
		unbind_cron(sv, name)
		register_cron(sv, s)
	}
	return ""
}

// config_cycle_check runs the sort with the edited spec in place and rolls the
// map back if it introduces a cycle.
config_cycle_check :: proc(cfg: ^config.Config, name: string, spec: config.ServiceConfig) -> string {
	prev, had := cfg.services[name]
	cfg.services[name] = spec
	_, err := config.topological_sort(cfg)
	if len(err) > 0 {
		if had {
			cfg.services[name] = prev
		} else {
			delete_key(&cfg.services, name)
		}
	}
	return err
}

// add_service registers a service that only exists in the config file so far.
add_service :: proc(sv: ^Supervisor, name: string, spec: config.ServiceConfig) -> string {
	prev_ctx := context
	context = runtime.default_context()
	defer context = prev_ctx
	sync.rw_mutex_lock(&sv.mu)
	defer sync.rw_mutex_unlock(&sv.mu)

	if _, exists := sv.services[name]; exists {
		return fmt.tprintf("service already exists: {}", name)
	}
	if _, exists := sv.cfg.services[name]; exists {
		return fmt.tprintf("service already exists: {}", name)
	}
	if len(name) == 0 {
		return "service name is required"
	}
	if err := validate_spec(sv.cfg, name, spec); len(err) > 0 {
		return err
	}
	if e := config_cycle_check(sv.cfg, name, spec); len(e) > 0 {
		return e
	}

	s := new(Service)
	s.name = strings.clone(name)
	s.spec = spec
	s.status = .Stopped
	s.exits = make([dynamic]i64)
	sv.services[s.name] = s
	append(&sv.order, s.name)
	config.insertion_sort_strings(sv.order[:])
	sv.cfg.services[s.name] = spec
	rebuild_order(sv)
	register_cron(sv, s)
	return ""
}

// remove_service drops a service from the config and stops it. The exit is
// observed by the loop: the entry stays in the table, flagged `removing`, until
// the child is reaped, otherwise waitpid would never run and the pid would turn
// into a zombie that nothing can clean up.
remove_service :: proc(sv: ^Supervisor, name: string) -> string {
	prev_ctx := context
	context = runtime.default_context()
	defer context = prev_ctx
	sync.rw_mutex_lock(&sv.mu)
	defer sync.rw_mutex_unlock(&sv.mu)

	s, ok := sv.services[name]
	if !ok {
		return fmt.tprintf("unknown service: {}", name)
	}
	if s.removing {
		return ""
	}
	s.removing = true
	append(&sv.removing, s.name)
	s.want_running = false
	s.restarting = false

	now := now_seconds(sv)
	if s.cron_pid > 0 {
		signal(s.cron_pid, .SIGTERM)
		s.cron_started = 0
	}
	if s.pid > 0 {
		signal(s.pid, .SIGTERM)
		s.kill_at = now + STOP_GRACE
	}

	unbind_cron(sv, name)
	if _, exists := sv.cfg.services[name]; exists {
		delete_key(&sv.cfg.services, name)
	}
	rebuild_order(sv)

	for n, i in sv.order {
		if n == name {
			ordered_remove(&sv.order, i)
			break
		}
	}

	if s.pid == 0 && s.cron_pid == 0 {
		destroy_service_locked(sv, s)
	}
	return ""
}

// reap_removed_locked reaps the children of services being deleted and forgets
// each entry once nothing is left to wait for. The names come from their own
// list rather than the table, so a delete never leaves a half-removed row in the
// map the loop is walking.
reap_removed_locked :: proc(sv: ^Supervisor, now: i64) {
	if len(sv.removing) == 0 {
		return
	}
	for i := len(sv.removing) - 1; i >= 0; i -= 1 {
		name := sv.removing[i]
		s, ok := sv.services[name]
		if !ok {
			ordered_remove(&sv.removing, i)
			continue
		}
		if s.pid > 0 {
			_ = poll_locked(sv, s, now)
		}
		if s.pid == 0 && s.cron_pid == 0 {
			destroy_service_locked(sv, s)
		}
	}
}

destroy_service_locked :: proc(sv: ^Supervisor, s: ^Service) {
	name := s.name
	delete_key(&sv.services, name)
	for n, i in sv.removing {
		if n == name {
			ordered_remove(&sv.removing, i)
			break
		}
	}
	delete(s.exits)
	delete(name)
	free(s)
}

// spec_of returns the stored config for one service.
spec_of :: proc(sv: ^Supervisor, name: string) -> (config.ServiceConfig, bool) {
	sync.rw_mutex_shared_lock(&sv.mu)
	defer sync.rw_mutex_shared_unlock(&sv.mu)
	s, ok := sv.services[name]
	if !ok {
		return {}, false
	}
	return s.spec, true
}

// save_config_file writes the in-memory config back to the file it came from.
save_config_file :: proc(sv: ^Supervisor) -> string {
	sync.rw_mutex_shared_lock(&sv.mu)
	defer sync.rw_mutex_shared_unlock(&sv.mu)
	return config.save_config(sv.cfg)
}
