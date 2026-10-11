package paths

import "core:os"
import "core:fmt"

// RuntimeDir is the base directory for all gorch runtime files (pid, socket, logs).
RuntimeDir :: "/tmp/gorch"

DefaultPIDFile      :: RuntimeDir + "/gorch.pid"
DefaultServicesLock :: RuntimeDir + "/gorch-services.lock"
DefaultSocketPath   :: RuntimeDir + "/gorch.sock"
DefaultLogDir       :: RuntimeDir + "/logs"

// ServicePidDir is where per-service pid files live.
ServicePidDir :: RuntimeDir + "/services"

// ensure_dir creates a directory (single level) if it does not already exist.
ensure_dir :: proc(path: string) -> os.Error {
	if os.exists(path) {
		return nil
	}
	return os.make_directory(path)
}

// EnsureRuntimeDir creates the runtime directory tree with default permissions.
// Mirrors config/paths.go EnsureRuntimeDir in the Go implementation.
ensure_runtime_dir :: proc() -> os.Error {
	dirs := []string{RuntimeDir, DefaultLogDir, ServicePidDir}
	for d in dirs {
		err := ensure_dir(d)
		if err != nil && !os.exists(d) {
			return err
		}
	}
	return nil
}

// LogPath returns the full path for a named log file inside DefaultLogDir.
log_path :: proc(name: string) -> string {
	return fmt.tprintf("{}/{}", DefaultLogDir, name)
}
