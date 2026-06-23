//go:build !unix

package supervisor

import (
	"os"
	"os/exec"
)

// setProcessGroup is a no-op on non-Unix platforms.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup falls back to killing just the single process on non-Unix.
func killProcessGroup(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}

// getProcessMemoryMB is not supported on non-Unix platforms.
func getProcessMemoryMB(pid int) int64 {
	return 0
}
