package server

import (
	"context"

	"github.com/azhai/gobus/log"
	"github.com/azhai/gorch/config"
	"github.com/azhai/gorch/cron"
	"github.com/azhai/gorch/status"
)

// SupervisorProvider is everything the web layer needs from the supervisor.
//
// It lives in its own file without a build tag so that both the normal build
// and the `noweb` build agree on the signature of NewServer.
type SupervisorProvider interface {
	GetStatus(name string) (status.ServiceStatus, bool)
	GetAllStatus() map[string]status.ServiceStatus
	GetConfig() *config.Config
	GetLogManager() *log.Manager
	GetCronScheduler() *cron.Scheduler
	GetHub() *Hub
	StartService(ctx context.Context, name string) error
	StopService(ctx context.Context, name string) error
	RestartService(ctx context.Context, name string) error
	UpdateServiceConfig(name string, svc config.ServiceConfig) error
	CreateService(name string, svc config.ServiceConfig) error
	DeleteService(name string) error
	SaveConfig() error
}
