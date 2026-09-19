package config

import (
	"fmt"
	"os"
)

// RuntimeDir is the single directory holding every runtime file gorch writes
// for itself: PID files, the services lock, the IPC socket and service logs.
//
// These used to be spread across /var/run and /var/log, which are root-owned
// on most systems (and on macOS require sudo). Keeping them together in one
// world-writable directory lets gorch run unprivileged and makes the state it
// owns easy to inspect and clean up.
const RuntimeDir = "/tmp/gorch"

// Default locations of gorch's own runtime files, all inside RuntimeDir.
const (
	// DefaultPIDFile is the supervisor's PID file.
	DefaultPIDFile = RuntimeDir + "/gorch.pid"
	// DefaultServicesLock records the PIDs of managed services so a restart can
	// adopt processes left running by a previous instance.
	DefaultServicesLock = RuntimeDir + "/gorch-services.lock"
	// DefaultSocketPath is the Unix socket used for CLI ↔ supervisor IPC.
	DefaultSocketPath = RuntimeDir + "/gorch.sock"
	// DefaultLogDir is where service stdout/stderr logs are written.
	DefaultLogDir = RuntimeDir + "/logs"
)

// EnsureRuntimeDir creates RuntimeDir and its log subdirectory if missing.
// Safe to call repeatedly and from multiple processes at once.
func EnsureRuntimeDir() error {
	for _, dir := range []string{RuntimeDir, DefaultLogDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create runtime dir %s: %w", dir, err)
		}
	}
	return nil
}
