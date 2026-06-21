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

	"github.com/azhai/gorch/internal/config"
)

// ServicePidDir is where per-service PID files are stored.
const ServicePidDir = "/tmp/gorch"

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
}

func StartProcess(ctx context.Context, svc config.ServiceConfig, name string) (*ProcessInfo, error) {
	if _, err := os.Stat(svc.WORK_DIR); os.IsNotExist(err) {
		return nil, fmt.Errorf("work dir not found: %s", svc.WORK_DIR)
	}

	parts := strings.Fields(svc.EXEC_CMD)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty EXEC_CMD for service '%s'", name)
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = svc.WORK_DIR
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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

func StopProcess(proc *ProcessInfo, timeout time.Duration) error {
	if proc.Cmd == nil || proc.Cmd.Process == nil {
		RemoveServicePidFile(proc.Name)
		return nil
	}

	if err := proc.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to service '%s': %w", proc.Name, err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := proc.Cmd.Process.Wait()
		done <- err
	}()

	select {
	case <-done:
		proc.CloseFiles()
	case <-time.After(timeout):
		if err := proc.Cmd.Process.Signal(syscall.SIGKILL); err != nil {
			return fmt.Errorf("failed to send SIGKILL to service '%s': %w", proc.Name, err)
		}
		<-done
		proc.CloseFiles()
	}

	RemoveServicePidFile(proc.Name)
	return nil
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

func EnsureDir(filePath string) error {
	dir := filepath.Dir(filePath)
	return os.MkdirAll(dir, 0755)
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
	syscall.Kill(-pid, syscall.SIGKILL) // kill process group
	RemoveServicePidFile(name)
	return true
}
