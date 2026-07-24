//go:build linux

package supervisor

import (
	"fmt"
	"os"
	"syscall"
)

// Daemonize forks the current process into a background daemon.
// Returns the child PID on success.
func Daemonize() (int, error) {
	childPid, err := syscall.ForkExec(
		os.Args[0], os.Args[1:],
		&syscall.ProcAttr{
			Env:   os.Environ(),
			Files: []uintptr{0, 1, 2},
			Sys:   &syscall.SysProcAttr{Setsid: true},
		},
	)
	if err != nil {
		return 0, fmt.Errorf("failed to fork: %w", err)
	}
	return childPid, nil
}
