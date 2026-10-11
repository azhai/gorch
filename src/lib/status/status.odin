package status

import "core:time"
import "core:sync"

// Status codes mirror the Go StatusCode values.
Status :: enum {
	Starting,
	Running,
	Stopped,
	Failed,
	Crashed,
}

status_to_string :: proc(s: Status) -> string {
	switch s {
	case .Starting: return "starting"
	case .Running:  return "running"
	case .Stopped:  return "stopped"
	case .Failed:   return "failed"
	case .Crashed:  return "crashed"
	}
	return "stopped"
}

ServiceStatus :: struct {
	name:       string,
	status:     Status,
	pid:        int,
	started_at: i64, // unix seconds
	uptime:     i64, // seconds
	memory_mb:  f64,
	exit_code:  ^int, // optional; nil when not exited
}

Cache :: struct {
	mu:       sync.RW_Mutex,
	statuses: map[string]ServiceStatus,
}

cache_init :: proc(c: ^Cache) {
	c.statuses = make(map[string]ServiceStatus)
}

compute_uptime :: proc(ss: ^ServiceStatus) {
	if ss.status == .Running && ss.started_at > 0 {
		now := time.to_unix_seconds(time.now())
		ss.uptime = now - ss.started_at
		if ss.uptime < 0 {
			ss.uptime = 0
		}
	} else {
		ss.uptime = 0
	}
}

// update replaces the stored status for a service (computing uptime).
update :: proc(c: ^Cache, ss: ServiceStatus) {
	sync.rw_mutex_lock(&c.mu)
	defer sync.rw_mutex_unlock(&c.mu)
	out := ss
	compute_uptime(&out)
	c.statuses[out.name] = out
}

// update_memory refreshes only the memory figure, preserving other fields.
update_memory :: proc(c: ^Cache, name: string, mem: f64) {
	sync.rw_mutex_lock(&c.mu)
	defer sync.rw_mutex_unlock(&c.mu)
	if ss, ok := c.statuses[name]; ok {
		ss.memory_mb = mem
		compute_uptime(&ss)
		c.statuses[name] = ss
	}
}

// update_process_info sets the pid; a changed pid resets started_at (new process).
update_process_info :: proc(c: ^Cache, name: string, pid: int) {
	sync.rw_mutex_lock(&c.mu)
	defer sync.rw_mutex_unlock(&c.mu)
	ss, ok := c.statuses[name]
	if !ok {
		return
	}
	if ss.pid != pid {
		ss.pid = pid
		ss.started_at = time.to_unix_seconds(time.now())
	}
	compute_uptime(&ss)
	c.statuses[name] = ss
}

get :: proc(c: ^Cache, name: string) -> (ServiceStatus, bool) {
	sync.rw_mutex_shared_lock(&c.mu)
	defer sync.rw_mutex_shared_unlock(&c.mu)
	ss, ok := c.statuses[name]
	if ok {
		compute_uptime(&ss)
	}
	return ss, ok
}

// get_all returns a snapshot slice of every service status.
get_all :: proc(c: ^Cache) -> []ServiceStatus {
	sync.rw_mutex_shared_lock(&c.mu)
	defer sync.rw_mutex_shared_unlock(&c.mu)
	d := [dynamic]ServiceStatus{}
	for _, ss in c.statuses {
		out := ss
		compute_uptime(&out)
		append(&d, out)
	}
	return d[:]
}

// set_status is a convenience used by the supervisor for coarse transitions.
set_status :: proc(c: ^Cache, name: string, s: Status) {
	sync.rw_mutex_lock(&c.mu)
	defer sync.rw_mutex_unlock(&c.mu)
	ss, ok := c.statuses[name]
	if !ok {
		ss.name = name
	}
	ss.status = s
	if s == .Running {
		ss.started_at = time.to_unix_seconds(time.now())
	}
	if s == .Stopped || s == .Failed || s == .Crashed {
		ss.pid = 0
	}
	compute_uptime(&ss)
	c.statuses[name] = ss
}
