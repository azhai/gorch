package web

import (
	"context"

	"github.com/azhai/gorch/internal/config"
	"github.com/azhai/gorch/internal/cron"
	"github.com/azhai/gorch/internal/logs"
	"github.com/azhai/gorch/internal/status"
	"github.com/gofiber/fiber/v3"
)

type SupervisorProvider interface {
	GetStatus(name string) (status.ServiceStatus, bool)
	GetAllStatus() map[string]status.ServiceStatus
	GetConfig() *config.Config
	GetLogManager() *logs.Manager
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

type Server struct {
	app        *fiber.App
	supervisor SupervisorProvider
	addr       string
}

func NewServer(addr string, sup SupervisorProvider) *Server {
	s := &Server{
		addr:       addr,
		supervisor: sup,
	}

	app := fiber.New(fiber.Config{
		AppName: "gorch",
	})

	app.Use(corsMiddleware())
	app.Use(authMiddleware(sup.GetConfig().Web))

	s.app = app
	s.setupRoutes()

	return s
}

func (s *Server) Start() error {
	return s.app.Listen(s.addr)
}

func (s *Server) Stop() {
	s.app.Shutdown()
}

func corsMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set("Access-Control-Allow-Origin", "*")
		c.Set("Access-Control-Allow-Methods", "GET,POST,PUT,OPTIONS")
		c.Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if c.Method() == "OPTIONS" {
			return c.SendStatus(204)
		}
		return c.Next()
	}
}
