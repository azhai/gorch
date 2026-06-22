package status

import (
	"github.com/azhai/gorch/internal/config"
)

type ServiceStatus struct {
	Name         string            `json:"name"`
	Status       config.StatusCode `json:"status"`
	Pid          int               `json:"pid"`
	Uptime       int64             `json:"uptime"`
	RestartCount int               `json:"restartCount"`
	ExitCode     *int              `json:"exitCode"`
	StartedAt    int64             `json:"startedAt,omitempty"`
}
