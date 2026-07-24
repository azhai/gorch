package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/azhai/gorch/config"
)

// ServicePidDir is where per-service PID files are stored.
const ServicePidDir = "/tmp/gorch"

// psInfo holds process info from a single ps call.
type psInfo struct {
	ppid    int
	state   string
	rssMB   int64
	etime   string
	command string
}

type ProcessInfo struct {
	Name       string
	Cmd        *exec.Cmd
	Pid        int
	StartTime  time.Time
	Status     config.StatusCode
	RestartCnt int
	ExitCode   int
	StdoutFile *os.File
	StderrFile *os.File
	ManualStop bool
	Adopted    bool // true if process was adopted from a previous run via PID file
}

func StartProcess(ctx context.Context, svc config.ServiceConfig, name string) (*ProcessInfo, error) {
	if _, err := os.Stat(svc.WORK_DIR); os.IsNotExist(err) {
		return nil, fmt.Errorf("work dir not found: %s", svc.WORK_DIR)
	}

	parts := strings.Fields(svc.EXEC_CMD)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty EXEC_CMD for service '%s'", name)
	}

	// Use exec.Command (not CommandContext) so the child process is NOT killed
	// when ctx is canceled. The supervisor's ctx is canceled on shutdown, but
	// managed services should keep running and be stopped explicitly via
	// stopService. Binding the child to ctx would cause SIGKILL on shutdown,
	// defeating the purpose of graceful supervision.
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = svc.WORK_DIR
	setProcessGroup(cmd)

	if len(svc.ENV_VARS) > 0 {
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}
		for k, v := range svc.ENV_VARS {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	proc := &ProcessInfo{
		Name:      name,
		Cmd:       cmd,
		Status:    config.StatusStarting,
		StartTime: time.Now(),
	}

	if svc.STDOUT != "" {
		f, err := os.OpenFile(svc.STDOUT, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open stdout file %s: %w", svc.STDOUT, err)
		}
		proc.StdoutFile = f
		cmd.Stdout = f
	}

	if svc.STDERR != "" {
		if svc.STDERR == svc.STDOUT && proc.StdoutFile != nil {
			cmd.Stderr = proc.StdoutFile
			proc.StderrFile = proc.StdoutFile
		} else {
			f, err := os.OpenFile(svc.STDERR, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				return nil, fmt.Errorf("failed to open stderr file %s: %w", svc.STDERR, err)
			}
			proc.StderrFile = f
			cmd.Stderr = f
		}
	}

	if err := cmd.Start(); err != nil {
		proc.CloseFiles()
		return nil, fmt.Errorf("failed to start service '%s': %w", name, err)
	}

	proc.Pid = cmd.Process.Pid
	proc.Status = config.StatusRunning

	if err := WriteServicePidFile(name, proc.Pid); err != nil {
		slog.Warn("failed to write pid file", "service", name, "error", err)
	}

	return proc, nil
}

// AdoptProcess creates a ProcessInfo for an already-running process identified by PID.
// It checks whether the process is alive. Returns nil if the process does not exist.
func AdoptProcess(name string, pid int) *ProcessInfo {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	// signal 0 checks process existence without actually sending a signal
	if err := p.Signal(syscall.Signal(0)); err != nil {
		return nil
	}
	return &ProcessInfo{
		Name:      name,
		Pid:       pid,
		Status:    config.StatusRunning,
		StartTime: time.Now(),
		Adopted:   true,
	}
}

func StopProcess(proc *ProcessInfo, timeout time.Duration) error {
	if proc.Cmd == nil || proc.Cmd.Process == nil {
		// For adopted processes (no Cmd), signal directly via PID
		if proc.Adopted && proc.Pid > 0 {
			p, err := os.FindProcess(proc.Pid)
			if err == nil {
				p.Signal(syscall.SIGTERM)
				// poll until exit or timeout
				deadline := time.Now().Add(timeout)
				for time.Now().Before(deadline) {
					if p.Signal(syscall.Signal(0)) != nil {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				// force kill if still alive
				if p.Signal(syscall.Signal(0)) == nil {
					killProcessGroup(proc.Pid)
				}
			}
		}
		RemoveServicePidFile(proc.Name)
		return nil
	}

	// SIGTERM the process group, then poll until exit or timeout
	proc.Cmd.Process.Signal(syscall.SIGTERM)
	if !waitExit(proc, timeout) {
		killProcessGroup(proc.Cmd.Process.Pid)
		waitExit(proc, 5*time.Second)
	}

	proc.CloseFiles()
	RemoveServicePidFile(proc.Name)
	return nil
}

// waitExit polls the process until it exits or deadline elapses.
func waitExit(proc *ProcessInfo, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if proc.Cmd.Process.Signal(syscall.Signal(0)) != nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func MonitorProcess(proc *ProcessInfo) (<-chan int, error) {
	ch := make(chan int, 1)

	go func() {
		defer close(ch)
		state, err := proc.Cmd.Process.Wait()
		if err != nil {
			ch <- -1
			return
		}
		exitCode := state.ExitCode()
		proc.ExitCode = exitCode
		ch <- exitCode
	}()

	return ch, nil
}

func (p *ProcessInfo) CloseFiles() {
	if p.StdoutFile != nil && p.StdoutFile != p.StderrFile {
		p.StdoutFile.Close()
	}
	if p.StderrFile != nil {
		p.StderrFile.Close()
	}
}

// servicePidPath returns the path for a service's PID file.
func servicePidPath(name string) string {
	return filepath.Join(ServicePidDir, name+".pid")
}

// WriteServicePidFile writes a service's PID to its PID file.
func WriteServicePidFile(name string, pid int) error {
	path := servicePidPath(name)
	if err := os.MkdirAll(ServicePidDir, 0755); err != nil {
		return fmt.Errorf("failed to create pid dir: %w", err)
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0644)
}

// ReadServicePidFile reads a service's PID from its PID file.
// Returns 0 if file doesn't exist or is invalid.
func ReadServicePidFile(name string) (int, error) {
	path := servicePidPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid in %s: %w", path, err)
	}
	return pid, nil
}

// RemoveServicePidFile removes a service's PID file.
func RemoveServicePidFile(name string) error {
	path := servicePidPath(name)
	return os.Remove(path)
}

// maxOrphanAge is how long a PID file can be considered fresh.
// Older files are treated as stale from a previous run.
const maxOrphanAge = 30 * time.Minute

// KillOrphanProcess checks if a PID file exists for a stale process and kills it.
// Returns true if an orphan was found and killed.
// Skips PID files older than maxOrphanAge to avoid killing legitimate processes.
func KillOrphanProcess(name string, supervisorPid int) bool {
	path := servicePidPath(name)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	// Skip old PID files - they may belong to a legitimate process
	if time.Since(info.ModTime()) > maxOrphanAge {
		return false
	}

	pid, err := ReadServicePidFile(name)
	if err != nil || pid <= 0 || pid == supervisorPid {
		return false
	}

	// Check if process still exists by sending signal 0
	p, err := os.FindProcess(pid)
	if err != nil {
		RemoveServicePidFile(name)
		return false
	}
	if err := p.Signal(syscall.Signal(0)); err != nil {
		// Process not running, clean up stale file
		RemoveServicePidFile(name)
		return false
	}

	// Process is running but we didn't start it - it's an orphan
	fmt.Printf("killing orphan process %s (pid=%d)\n", name, pid)
	killProcessGroup(pid)
	RemoveServicePidFile(name)
	return true
}
