package status

import (
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

	c.statuses[name] = status
}

func (c *Cache) Get(name string) (ServiceStatus, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	st, ok := c.statuses[name]
	if ok {
		st = computeUptime(st)
	}
	return st, ok
}

func (c *Cache) GetAll() map[string]ServiceStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]ServiceStatus, len(c.statuses))
	for k, v := range c.statuses {
		result[k] = computeUptime(v)
	}
	return result
}

func computeUptime(st ServiceStatus) ServiceStatus {
	if st.Status == config.StatusRunning && st.StartedAt > 0 {
		st.Uptime = int64(time.Since(time.Unix(st.StartedAt, 0)).Seconds())
	}
	return st
}
