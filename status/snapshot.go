package status

import (
	"github.com/azhai/gorch/config"
)

type ServiceStatus struct {
	Name      string            `json:"name"`
	Status    config.StatusCode `json:"status"`
	Pid       int               `json:"pid"`
	Uptime    int64             `json:"uptime"`
	MemoryMB  int64             `json:"memoryMB"`
	ExitCode  *int              `json:"exitCode"`
	StartedAt int64             `json:"startedAt,omitempty"`
}
