package supervisor

import "base:runtime"
import "core:os"
import "core:strings"
import "core:sys/posix"
import "core:time"

// Process helpers. Everything here is posix-only; gorch targets macOS and Linux
// (the Makefile cross-builds musl), so Windows is out of scope for now.

// alive reports whether the pid still exists. Signal 0 only validates, it delivers
// nothing. A zombie still answers true, so children we spawned are reaped through
// os.process_wait instead of being polled with this.
alive :: proc(pid: int) -> bool {
	if pid <= 0 {
		return false
	}
	return posix.kill(posix.pid_t(pid), .NONE) == .OK
}

signal :: proc(pid: int, sig: posix.Signal) -> bool {
	if pid <= 0 {
		return false
	}
	return posix.kill(posix.pid_t(pid), sig) == .OK
}

// SHELL_META are the characters that force the command to stay inside the shell.
// For a plain command we prefix `exec` so the shell replaces itself: the pid we
// track is then the service itself, not a wrapper, which matters for kill/reap.
SHELL_META :: "|&;<>()$`*?[]{}~"

COMMAND_MAX :: 1024

// wrap_command writes the command line into `buf` and returns a string aliasing it,
// so the caller must keep `buf` alive until the process is spawned. It deliberately
// avoids fmt.tprintf: a temporary-allocator string must not outlive the statement
// that produced it, and the descriptor is filled in on the next line.
wrap_command :: proc(buf: []u8, cmd: string) -> (string, bool) {
	needs_shell := strings.contains_any(cmd, SHELL_META)
	prefix := needs_shell ? "" : "exec "
	total := len(prefix) + len(cmd)
	if total > len(buf) || total == 0 {
		return "", false
	}
	n := copy(buf, prefix)
	n += copy(buf[n:], cmd)
	return transmute(string)buf[:n], true
}

// run_shell runs a script (PRE_ACTION, RESTART_CMD) and waits for it. It
// deliberately does not use `exec`: these commands are usually pipelines.
// `env` is optional; see child_env for how it is built.
run_shell :: proc(script, dir: string, timeout: time.Duration, env: []string = nil) -> bool {
	if script == "" {
		return true
	}
	p, err := os.process_start(
		os.Process_Desc {
			working_dir = dir,
			command     = []string{"/bin/sh", "-c", script},
			env         = env,
		},
	)
	if err != nil {
		return false
	}
	state, werr := os.process_wait(p, timeout)
	if werr != nil {
		_ = os.process_kill(p)
		return false
	}
	return state.success
}

// ---- ENV_VARS ----
//
// os.Process_Desc.env is a *replacement* environment: nil means "inherit from
// gorch", anything else (including an empty slice) is taken literally. So to
// overlay ENV_VARS we have to reproduce the parent environment first and drop
// any key that the service overrides, then append the overrides. Appending
// duplicates is not enough: getenv() returns the first match, not the last.
Child_Env :: struct {
	items:     [dynamic]string, // full env handed to the child
	base:      []string, // owned strings from os.environ, borrowed by items
	added:     int, // trailing entries in items that we allocated ourselves
	allocator: runtime.Allocator,
}

env_key :: proc(entry: string) -> string {
	if i := strings.index_byte(entry, '='); i > 0 {
		return entry[:i]
	}
	return entry
}

child_env :: proc(vars: map[string]string) -> Child_Env {
	out := Child_Env{allocator = context.allocator}
	if len(vars) == 0 {
		return out // nil -> inherit, no allocations
	}
	base, err := os.environ(out.allocator)
	if err != nil {
		return out
	}
	out.base = base
	out.items = make([dynamic]string, 0, len(base) + len(vars), out.allocator)
	for entry in base {
		if _, overridden := vars[env_key(entry)]; overridden {
			continue
		}
		append(&out.items, entry)
	}
	for k, v in vars {
		append(&out.items, strings.concatenate({k, "=", v}, out.allocator))
		out.added += 1
	}
	return out
}

// child_env_view returns nil when there is nothing to overlay, which is the
// only way to tell os.process_start to inherit the parent environment.
child_env_view :: proc(env: ^Child_Env) -> []string {
	if len(env.items) == 0 {
		return nil
	}
	return env.items[:]
}

child_env_destroy :: proc(env: ^Child_Env) {
	// All of these allocations came from child_env's allocator, which may be a
	// request arena rather than the current thread's heap.
	prev := context
	context.allocator = env.allocator
	defer context = prev
	for i := len(env.items) - env.added; i < len(env.items); i += 1 {
		delete(env.items[i])
	}
	if env.items != nil {
		delete(env.items)
	}
	for entry in env.base {
		delete(entry)
	}
	if len(env.base) > 0 {
		delete(env.base)
	}
	env.items = nil
	env.base = nil
	env.added = 0
}
