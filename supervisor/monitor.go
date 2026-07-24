package supervisor

import (
	"context"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/azhai/gorch/config"
	"github.com/azhai/gorch/status"
)

// detectDaemonize waits briefly after process start to detect if the process
// daemonized (forked a new master and exited the original PID).
// Returns the new master PID if detected, 0 otherwise.
// When PID_FILE is configured, reads the PID directly from the file instead of
// using findMainProcessByName's complex pgrep + PPID heuristic.
//
// With PID_FILE configured, we trust the PID file and read the real PID directly,
// without waiting for the original process to exit (angie may not exit the parent).
// Without PID_FILE, fall back to the original wait+find approach.
func (s *Supervisor) detectDaemonize(proc *ProcessInfo, svc config.ServiceConfig) int {
	originalPid := proc.Pid

	// With PID_FILE, read the PID directly without waiting for the original process to exit.
	// Angie and similar daemons may keep the parent process alive or use a different pattern.
	if svc.PID_FILE != "" {
		found := readUserPidFile(svc.PID_FILE, proc.StartTime)
		if found > 0 && found != originalPid {
			slog.Info("detected daemonize via PID_FILE, switching to new master PID",
				"service", proc.Name, "oldPid", originalPid, "newPid", found)
			return found
		}
		// PID_FILE didn't yield a different PID. If the original process is still running,
		// maybe it's not a daemonize pattern — just use the original PID.
		if found == 0 {
			slog.Warn("PID_FILE configured but couldn't read PID, using original PID",
				"service", proc.Name, "pidFile", svc.PID_FILE)
		}
		return 0
	}

	// Without PID_FILE, use the original approach: wait for the original process to exit.
	if proc.Cmd == nil || proc.Cmd.Process == nil {
		return 0
	}

	// Wait up to 500ms for the original process to exit (daemonize pattern).
	for i := 0; i < 10; i++ {
		time.Sleep(50 * time.Millisecond)
		if proc.Cmd.Process.Signal(syscall.Signal(0)) != nil {
			// Original process has exited — find the new master.
			found := findMainProcessByName(svc.EXEC_CMD)
			if found > 0 && found != originalPid {
				slog.Info("detected daemonize, switching to new master PID",
					"service", proc.Name, "oldPid", originalPid, "newPid", found)
				return found
			}
			// Original exited but no replacement found — not a daemonize, just a crash.
			return 0
		}
	}
	return 0
}

// findDaemonizedMaster checks if a process that just exited has a replacement
// (daemonize pattern). Checks PID_FILE first, then falls back to findMainProcessByName.
func findDaemonizedMaster(proc *ProcessInfo, svc config.ServiceConfig) int {
	if svc.PID_FILE != "" {
		if pid := tryReadUserPidFile(svc.PID_FILE); pid > 0 && pid != proc.Pid {
			return pid
		}
	}
	found := findMainProcessByName(svc.EXEC_CMD)
	if found > 0 && found != proc.Pid {
		return found
	}
	return 0
}

func (s *Supervisor) monitorLoop(ctx context.Context, name string, svc config.ServiceConfig) {
	defer s.wg.Done()

	proc, exists := s.processes[name]
	if !exists {
		return
	}

	exitCh, err := MonitorProcess(proc)
	if err != nil {
		slog.Error("monitor setup failed", "service", name, "error", err)
		return
	}

	exitCode := <-exitCh
	slog.Info("service exited", "service", name, "exitCode", exitCode)

	// Check manual stop without holding the lock (set by stopService before we get here)
	if proc.ManualStop {
		return
	}

	// Check if the process daemonized (forked a new master and exited the original
	// PID). This handles cases where detectDaemonize's 500ms timeout was too short.
	if newPid := findDaemonizedMaster(proc, svc); newPid > 0 {
		s.mu.Lock()
		if proc.ManualStop {
			s.mu.Unlock()
			return
		}
		slog.Info("process daemonized, switching to new master PID",
			"service", name, "oldPid", proc.Pid, "newPid", newPid)
		proc.Pid = newPid
		proc.Adopted = true
		proc.Cmd = nil
		WriteServicePidFile(name, newPid)
		treeMB := getProcessTreeMemoryMB(newPid)
		s.statusCache.Update(name, status.ServiceStatus{
			Name:      name,
			Status:    config.StatusRunning,
			Pid:       newPid,
			StartedAt: proc.StartTime.Unix(),
			MemoryMB:  treeMB,
		})
		s.hub.BroadcastStatusChange(name, string(config.StatusRunning), newPid, proc.StartTime.Unix(), treeMB)
		s.mu.Unlock()
		s.wg.Add(1)
		go s.monitorAdoptedLoop(ctx, name, svc)
		return
	}

	s.mu.Lock()
	// Re-check: stopService may have set ManualStop while we waited for the lock
	if proc.ManualStop {
		s.mu.Unlock()
		return
	}

	s.handleExited(ctx, name, svc, proc, exitCode)
}

// handleExited applies restart policy after a process exits.
// Caller must hold s.mu. Returns true if the service was restarted (caller should return).
func (s *Supervisor) handleExited(ctx context.Context, name string, svc config.ServiceConfig, proc *ProcessInfo, exitCode int) {
	shouldRestart := false
	switch config.RestartPolicy(svc.RESTART_POLICY) {
	case config.RestartAlways:
		shouldRestart = true
	case config.RestartOnFailure:
		shouldRestart = exitCode != 0
	case config.RestartNever:
		shouldRestart = false
	}

	if shouldRestart && proc.RestartCnt < maxRestartCount {
		restartCnt := proc.RestartCnt + 1
		// Remove old proc before starting new one
		delete(s.processes, name)
		s.mu.Unlock()

		// Skip restart if supervisor is shutting down
		if ctx.Err() != nil {
			slog.Info("context canceled, skipping restart", "service", name)
			return
		}

		// Force back-off to allow port/resource release; default 2s if unset
		backOff := svc.BACK_OFF
		if backOff <= 0 {
			backOff = 2
		}
		slog.Info("backing off before restart", "service", name, "backOffSeconds", backOff, "restartCount", restartCnt)
		time.Sleep(time.Duration(backOff) * time.Second)

		// Re-check after sleep: context may have been canceled during backoff
		if ctx.Err() != nil {
			slog.Info("context canceled during backoff, skipping restart", "service", name)
			return
		}

		if err := s.startService(ctx, name, svc); err != nil {
			slog.Error("restart failed", "service", name, "error", err)
		} else if newProc, ok := s.processes[name]; ok {
			newProc.RestartCnt = restartCnt
		}
		return
	}

	delete(s.processes, name)
	finalStatus := config.StatusStopped
	if exitCode != 0 {
		if proc.RestartCnt >= maxRestartCount {
			finalStatus = config.StatusCrashed
		} else {
			finalStatus = config.StatusFailed
		}
	}

	s.statusCache.Update(name, status.ServiceStatus{
		Name:     name,
		Status:   finalStatus,
		ExitCode: &exitCode,
	})
	s.hub.BroadcastStatusChange(name, string(finalStatus), 0, 0, 0)
	s.mu.Unlock()
}

// monitorAdoptedLoop polls an adopted process (no exec.Cmd) via signal 0.
// When the process disappears, it applies the same restart logic as monitorLoop.
func (s *Supervisor) monitorAdoptedLoop(ctx context.Context, name string, svc config.ServiceConfig) {
	defer s.wg.Done()

	proc, exists := s.processes[name]
	if !exists {
		return
	}

	pid := proc.Pid
	p, _ := os.FindProcess(pid)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.Signal(syscall.Signal(0)) == nil {
				continue
			}
			// process is gone
			s.handleAdoptedExit(ctx, name, svc, proc, pid)
			return
		}
	}
}

// handleAdoptedExit processes the exit of an adopted process.
func (s *Supervisor) handleAdoptedExit(ctx context.Context, name string, svc config.ServiceConfig, proc *ProcessInfo, pid int) {
	exitCode := -1
	slog.Info("adopted service exited", "service", name, "pid", pid)

	if proc.ManualStop {
		return
	}

	s.mu.Lock()
	if proc.ManualStop {
		s.mu.Unlock()
		return
	}
	s.handleExited(ctx, name, svc, proc, exitCode)
}
