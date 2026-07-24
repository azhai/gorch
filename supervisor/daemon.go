package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// lockFile holds the open file descriptor for the flock-based mutex.
var lockFile *os.File

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
