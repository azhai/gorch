//go:build unix

package supervisor

import (
	"os/exec"
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
