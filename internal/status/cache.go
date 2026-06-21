package status

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/azhai/gorch/internal/config"
)

type Cache struct {
	statuses map[string]ServiceStatus
	mu       sync.RWMutex
}

func NewCache() *Cache {
	return &Cache{
		statuses: make(map[string]ServiceStatus),
	}
}

func (c *Cache) Update(name string, status ServiceStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if status.Name == "" {
		status.Name = name
	}

	if status.Status == config.StatusRunning && status.Uptime == 0 {
		status.Uptime = 0
	}

	c.statuses[name] = status
}

func (c *Cache) Get(name string) (ServiceStatus, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	st, ok := c.statuses[name]
	return st, ok
}

func (c *Cache) GetAll() map[string]ServiceStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]ServiceStatus, len(c.statuses))
	for k, v := range c.statuses {
		result[k] = v
	}
	return result
}

type StateFile struct {
	Services map[string]ServiceState `json:"services"`
}

type ServiceState struct {
	Status       config.StatusCode `json:"status"`
	Pid          int               `json:"pid"`
	RestartCount int               `json:"restartCount"`
	ExitCode     *int              `json:"exitCode"`
	StartedAt    *time.Time        `json:"startedAt,omitempty"`
}

func SaveState(path string, services map[string]ServiceStatus) error {
	stateFile := StateFile{
		Services: make(map[string]ServiceState),
	}

	for name, st := range services {
		stateFile.Services[name] = ServiceState{
			Status:       st.Status,
			Pid:          st.Pid,
			RestartCount: st.RestartCount,
			ExitCode:     st.ExitCode,
		}
	}

	data, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

func LoadState(path string) (map[string]ServiceState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var stateFile StateFile
	if err := json.Unmarshal(data, &stateFile); err != nil {
		return nil, err
	}

	return stateFile.Services, nil
}
