package status

import "core:time"
import "core:sync"

// Status codes mirror the Go StatusCode values.
//
// Stopping is appended rather than inserted so the ordinals of the older
// values stay put: state.Record stores the ordinal as a raw byte, and a state
// file written by an older build must still decode.
Status :: enum {
	Starting,
	Running,
	Stopped,
	Failed,
	Crashed,
	// Stopping is the window between "the operator asked for this to stop" and
	// "the process is actually gone". The supervisor cannot shrink that window:
	// SIGTERM has to be sent, and the exit is only observed on the next poll.
	// Without a status for it, an API read straight after Stop still answered
	// `running`, and the dashboard looked like the click had done nothing.
	Stopping,
}

status_to_string :: proc(s: Status) -> string {
	switch s {
	case .Starting: return "starting"
	case .Running:  return "running"
	case .Stopped:  return "stopped"
	case .Failed:   return "failed"
	case .Crashed:  return "crashed"
	case .Stopping: return "stopping"
	}
	return "stopped"
}

// has_process reports whether the status still refers to a live child. A
// stopping process is alive until its exit is reaped, so it keeps its pid,
// its memory figure and its age.
has_process :: proc(s: Status) -> bool {
	return s == .Running || s == .Starting || s == .Stopping
}

ServiceStatus :: struct {
	name:       string,
	status:     Status,
	pid:        int,
	started_at: i64, // unix seconds
	uptime:     i64, // seconds; meaningful only when has_uptime is set
	has_uptime: bool, // false when there is no pid to measure uptime against
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

// compute_uptime fills uptime only when there is a live pid and a start time.
// A service can be marked running while its pid is unknown (a daemon that has
// not been located yet, or one that exited and is between restarts); displaying
// an age for it would be a lie, so has_uptime stays false and the API sends
// null. The UI renders that as a dash.
//
// A stopping service still has an age: its child is alive until SIGTERM lands,
// and dropping the figure mid-teardown makes the card flicker from "3h 12m" to
// "-" a moment before it reads "stopped".
compute_uptime :: proc(ss: ^ServiceStatus) {
	ss.has_uptime = false
	if ss.status == .Running || ss.status == .Stopping {
		if ss.pid > 0 && ss.started_at > 0 {
			now := time.to_unix_seconds(time.now())
			ss.uptime = now - ss.started_at
			if ss.uptime < 0 {
				ss.uptime = 0
			}
			ss.has_uptime = true
			return
		}
	}
	ss.uptime = 0
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
