package supervisor

// Finding the real process behind a daemonized service.
//
// nginx and angie fork a master and let the process gorch started exit (or, in
// angie's case, may keep the parent around). What gorch tracks is then either a
// dead shell or a parent that is not the pid operators care about. The Go build
// solved this with detectDaemonize + findMainProcessByName; this file is the
// same idea, without adding a dependency: `pgrep -x` lists candidates and `ps`
// tells us which one was reparented to init (PPID 1), the signature of a master
// that daemonized.

import "core:fmt"
import "core:os"
import "core:strconv"
import "core:strings"
import "core:sys/posix"
import "core:time"

PGREP_PATH :: "/usr/bin/pgrep"
PS_PATH :: "/bin/ps"

// capture_output runs argv and returns its stdout. Only pgrep/ps are run here
// and their output is far below the pipe buffer, so waiting before reading
// cannot deadlock. The caller owns the returned bytes.
capture_output :: proc(argv: []string, timeout: time.Duration) -> ([]byte, bool) {
	r, w, perr := os.pipe()
	if perr != nil {
		return nil, false
	}
	p, err := os.process_start(os.Process_Desc{command = argv, stdout = w})
	os.close(w)
	if err != nil {
		os.close(r)
		return nil, false
	}
	if _, werr := os.process_wait(p, timeout); werr != nil {
		_ = os.process_kill(p)
	}
	data, derr := os.read_entire_file(r, context.allocator)
	os.close(r)
	if derr != nil {
		return nil, false
	}
	return data, true
}

// exec_basename extracts the executable name from EXEC_CMD, so it can be fed to
// `pgrep -x`. It understands the `exec ` prefix and a quoted first argument,
// which is what real configs contain; shell wrappers such as `env` or `nice`
// are not unwrapped (neither does the Go build).
exec_basename :: proc(cmd: string) -> string {
	s := strings.trim_space(cmd)
	if strings.has_prefix(s, "exec ") {
		s = strings.trim_space(s[len("exec "):])
	}
	if len(s) == 0 {
		return ""
	}
	if i := strings.index_byte(s, ' '); i >= 0 {
		s = s[:i]
	}
	s = strings.trim(s, `"'`)
	if len(s) == 0 {
		return ""
	}
	return os.base(s)
}

// current_pid lets the supervisor refuse a PID_FILE that points at gorch
// itself; adopting our own pid would make `stop` kill the supervisor.
current_pid :: proc() -> int {
	return int(posix.getpid())
}

// process_parent returns the parent pid, or 0 when it cannot be read.
process_parent :: proc(pid: int) -> int {
	if pid <= 1 {
		return 0
	}
	arg := fmt.aprintf("%d", pid)
	defer delete(arg)
	out, ok := capture_output({PS_PATH, "-o", "ppid=", "-p", arg}, 2 * time.Second)
	if !ok {
		return 0
	}
	defer delete(out)
	ppid, pok := strconv.parse_i64(strings.trim_space(string(out)))
	if !pok || ppid < 0 {
		return 0
	}
	return int(ppid)
}

// find_main_process locates a daemonized master by executable name. A process
// reparented to init (PPID 1) wins; otherwise a single unambiguous match is
// accepted. Returning 0 rather than "smallest pid" is deliberate: with several
// same-named processes (a system nginx, another service) the smallest pid is a
// guess that can hand us somebody else's process.
find_main_process :: proc(cmd: string, exclude: []int) -> int {
	name := exec_basename(cmd)
	if len(name) == 0 {
		return 0
	}
	out, ok := capture_output({PGREP_PATH, "-x", name}, 2 * time.Second)
	if !ok {
		return 0
	}
	defer delete(out)

	lines, _ := strings.split(string(out), "\n")
	defer delete(lines)

	first := 0
	matches := 0
	for line in lines {
		pid64, pok := strconv.parse_i64(strings.trim_space(line))
		if !pok || pid64 <= 1 {
			continue
		}
		pid := int(pid64)
		excluded := false
		for e in exclude {
			if e == pid {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		matches += 1
		if first == 0 {
			first = pid
		}
		if process_parent(pid) == 1 {
			return pid
		}
	}
	return matches == 1 ? first : 0
}

// master_started_near guards the pgrep fallback: a process with the same name
// that was already running before this service started is somebody else's
// daemon (a system nginx, an instance managed by another supervisor), and
// adopting it would let a later `stop` kill an unrelated service. PID_FILE is
// exempt because the operator named that pid explicitly.
master_started_near :: proc(pid: int, launched_at: i64) -> bool {
	start, ok := proc_start_time(pid)
	if !ok {
		return false
	}
	return start >= launched_at - 10 && start <= launched_at + 10
}

// read_pid_file reads a plain-text pid file such as /var/run/nginx.pid.
read_pid_file :: proc(path: string) -> int {
	if len(path) == 0 {
		return 0
	}
	data, err := os.read_entire_file_from_path(path, context.allocator)
	if err != nil {
		return 0
	}
	defer delete(data)
	pid64, ok := strconv.parse_i64(strings.trim_space(string(data)))
	if !ok || pid64 <= 1 {
		return 0
	}
	return int(pid64)
}
