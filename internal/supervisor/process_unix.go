//go:build unix

package supervisor

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// readProcMemory reads memory usage (RSS in MB) directly from /proc/<pid>/statm.
// Only works on Linux. Returns 0 on other platforms or if the file can't be read.
func readProcMemory(pid int) int64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
	if err != nil {
		return 0
	}
	f := strings.Fields(string(data))
	if len(f) < 2 {
		return 0
	}
	resident, err := strconv.Atoi(f[1])
	if err != nil {
		return 0
	}
	const pageSize = 4096 // bytes
	return int64(resident) * pageSize / 1024 / 1024
}

// setProcessGroup configures the command to run in its own process group.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the process group of the given PID.
func killProcessGroup(pid int) {
	syscall.Kill(-pid, syscall.SIGKILL)
}

// getProcessInfo fetches ppid/state/etime/rss/command via one ps call.
// state is empty if the process does not exist.
func getProcessInfo(pid int) (info psInfo) {
	out, err := exec.Command("ps", "-o", "ppid=,state=,etime=,rss=,command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return
	}
	f := strings.Fields(string(out))
	if len(f) < 4 {
		return
	}
	info.ppid, _ = strconv.Atoi(f[0])
	info.state = f[1]
	info.etime = f[2]
	rss, _ := strconv.Atoi(f[3])
	info.rssMB = int64(rss) / 1024
	if len(f) > 4 {
		info.command = strings.Join(f[4:], " ")
	}
	return
}

// getProcessTreeMemoryMB returns total RSS (in MB) of the process and all its descendants.
// Uses pgrep -P to recursively walk the process tree.
func getProcessTreeMemoryMB(pid int) int64 {
	var total int64
	seen := make(map[int]bool)
	var walk func(int)
	walk = func(p int) {
		if p <= 0 || seen[p] {
			return
		}
		seen[p] = true
		info := getProcessInfo(p)
		if info.state == "" {
			return
		}
		total += info.rssMB
		// find children
		out, err := exec.Command("pgrep", "-P", strconv.Itoa(p)).Output()
		if err != nil {
			return
		}
		for _, s := range strings.Fields(string(out)) {
			if child, err := strconv.Atoi(s); err == nil {
				walk(child)
			}
		}
	}
	walk(pid)
	return total
}

// findMainProcessByName locates the main process PID by matching the executable name.
// Useful for daemons (e.g. nginx/angie) that fork a new master and exit the original PID,
// leaving the real master reparented to init (PPID=1). Workers are children of the master,
// so they have PPID=master. Strategy:
//  1. Among matches, prefer the one with PPID=1 (daemonized master reparented to init).
//  2. Fall back to the smallest PID (master starts first).
func findMainProcessByName(execCmd string) int {
	parts := strings.Fields(execCmd)
	if len(parts) == 0 {
		return 0
	}
	name := filepath.Base(parts[0])
	out, err := exec.Command("pgrep", "-x", name).Output()
	if err != nil {
		return 0
	}
	var pids []int
	for _, s := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(s); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	if len(pids) == 0 {
		return 0
	}
	// Prefer the process with PPID=1 (daemonized master reparented to init).
	for _, pid := range pids {
		if info := getProcessInfo(pid); info.ppid == 1 {
			return pid
		}
	}
	// Fall back to smallest PID (master starts first).
	minPid := pids[0]
	for _, pid := range pids[1:] {
		if pid < minPid {
			minPid = pid
		}
	}
	return minPid
}

// killPortProcess kills any process listening on the given port.
func killPortProcess(port int) {
	out, err := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port)).Output()
	if err != nil {
		return
	}
	for _, s := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(s); err == nil && pid > 0 {
			slog.Info("killing port owner", "port", port, "pid", pid)
			killProcessGroup(pid)
		}
	}
}

// readUserPidFile reads a PID from a plain text PID file (e.g. /var/run/nginx.pid).
// Waits up to 5 seconds for the file to appear and contain a valid PID.
// Checks that the PID file's mtime is after procStartTime to avoid reading stale PID files
// from a previous run. After reading a valid PID, waits 5 seconds and re-reads to verify
// the PID hasn't changed (process didn't restart).
// Returns 0 if the file doesn't exist, is invalid, or the PID changed during verification.
func readUserPidFile(path string, procStartTime time.Time) int {
	if path == "" {
		return 0
	}
	// Two-phase approach:
	//  Phase 1: wait for PID file to appear with mtime after procStartTime (up to 5s)
	//  Phase 2: after getting a PID, wait 5s and re-read to verify it hasn't changed
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, err := os.Stat(path)
		if err == nil {
			mtime := info.ModTime()
			// Only trust the PID file if it was written after the process started.
			// Allow 1 second tolerance for fast-starting processes.
			if mtime.After(procStartTime.Add(-1 * time.Second)) {
				data, err := os.ReadFile(path)
				if err == nil {
					if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
						slog.Info("readUserPidFile: got candidate PID, verifying", "path", path, "pid", pid)
						// Got a candidate PID. Wait 5s then re-read to verify.
						time.Sleep(5 * time.Second)
						// Re-read and check if PID changed.
						newPid := tryReadUserPidFile(path)
						if newPid == pid {
							// PID unchanged after 5s — trustworthy.
							slog.Info("readUserPidFile: PID verified, returning", "path", path, "pid", pid)
							return pid
						}
						// PID changed — file is unstable, restart the wait.
						slog.Info("PID file changed during verification, retrying",
							"path", path, "oldPid", pid, "newPid", newPid)
						deadline = time.Now().Add(5 * time.Second)
						continue
					}
				}
			} else {
				slog.Debug("PID file mtime too early, waiting", "path", path, "mtime", mtime, "procStartTime", procStartTime)
			}
		}
		if time.Now().After(deadline) {
			slog.Info("readUserPidFile: deadline exceeded, returning 0", "path", path, "procStartTime", procStartTime)
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// tryReadUserPidFile reads a PID from a plain text PID file without waiting.
// Returns 0 if the file doesn't exist or is invalid.
func tryReadUserPidFile(path string) int {
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// runPreAction executes a shell command before starting the service.
// workDir is used as the working directory if non-empty.
func runPreAction(cmdLine, workDir string) error {
	cmd := exec.Command("sh", "-c", cmdLine)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
