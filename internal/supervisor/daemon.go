package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"time"
)

type PidFile struct {
	SupervisorPid int            `json:"supervisorPid"`
	Services      map[string]int `json:"services"`
	ConfigPath    string         `json:"configPath"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

func WritePidFile(path string, supervisorPid int, services map[string]int) error {
	pidFile := PidFile{
		SupervisorPid: supervisorPid,
		Services:      services,
		UpdatedAt:     time.Now(),
	}

	data, err := json.MarshalIndent(pidFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal PID file: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	return nil
}

func ReadPidFile(path string) (*PidFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read PID file: %w", err)
	}

	var pidFile PidFile
	if err := json.Unmarshal(data, &pidFile); err != nil {
		return nil, fmt.Errorf("failed to parse PID file: %w", err)
	}

	return &pidFile, nil
}

func AcquireLock(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return fmt.Errorf("another instance is running")
	}

	return nil
}

func ReleaseLock(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return nil
	}
	defer f.Close()

	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	os.Remove(path)
	return nil
}

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
