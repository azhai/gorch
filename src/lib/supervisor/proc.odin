package supervisor

import "core:fmt"
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

// run_shell runs a script (PRE_ACTION) and waits for it. It deliberately does not
// use `exec`: these commands are usually pipelines.
run_shell :: proc(script, dir: string, timeout: time.Duration) -> bool {
	if script == "" {
		return true
	}
	p, err := os.process_start(
		os.Process_Desc{working_dir = dir, command = []string{"/bin/sh", "-c", script}},
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
