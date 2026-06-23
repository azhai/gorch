//go:build unix

package supervisor

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// setProcessGroup configures the command to run in its own process group.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the process group of the given PID.
func killProcessGroup(pid int) {
	syscall.Kill(-pid, syscall.SIGKILL)
}

// getProcessMemoryMB returns the RSS memory usage of a process in MB.
// Uses `ps` command for portability across Unix systems.
func getProcessMemoryMB(pid int) int64 {
	data, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	rssKB, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return int64(rssKB) / 1024
}
