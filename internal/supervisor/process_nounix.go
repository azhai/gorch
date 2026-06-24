//go:build !unix

package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// setProcessGroup is a no-op on non-Unix platforms.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup falls back to killing just the single process on non-Unix.
func killProcessGroup(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}

// getProcessInfo is unsupported on non-Unix platforms.
func getProcessInfo(pid int) (info psInfo) {
	return
}

// getProcessTreeMemoryMB is unsupported on non-Unix platforms.
func getProcessTreeMemoryMB(pid int) int64 {
	return 0
}

// findMainProcessByName is unsupported on non-Unix platforms.
func findMainProcessByName(execCmd string) int {
	return 0
}

// killPortProcess is unsupported on non-Unix platforms.
func killPortProcess(port int) {}

// readUserPidFile is unsupported on non-Unix platforms.
func readUserPidFile(path string, procStartTime time.Time) int {
	return 0
}

// tryReadUserPidFile is unsupported on non-Unix platforms.
func tryReadUserPidFile(path string) int {
	return 0
}

// readProcMemory is unsupported on non-Unix platforms.
func readProcMemory(pid int) int64 {
	return 0
}

// runPreAction is unsupported on non-Unix platforms.
func runPreAction(cmdLine, workDir string) error {
	return fmt.Errorf("pre-action unsupported on this platform")
}
