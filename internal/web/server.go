package web

import (
	"context"

	"github.com/azhai/gorch/internal/config"
	"github.com/azhai/gorch/internal/cron"
	"github.com/azhai/gorch/internal/logs"
	"github.com/azhai/gorch/internal/status"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
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
	app        *echo.Echo
	supervisor SupervisorProvider
	addr       string
}

func NewServer(addr string, sup SupervisorProvider) *Server {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "OPTIONS", "DELETE"},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: false,
	}))

	e.Use(authMiddleware(sup.GetConfig().Web))

	s := &Server{
		app:        e,
		supervisor: sup,
		addr:       addr,
	}

	s.setupRoutes()
	return s
}

func (s *Server) Start() error {
	return s.app.Start(s.addr)
}

func (s *Server) Stop() {
	ctx := context.Background()
	s.app.Shutdown(ctx)
}

func (s *Server) setupRoutes() {
	api := s.app.Group("/api")

	api.POST("/auth/login", s.handleLogin)
	api.GET("/services", s.handleGetServices)
	api.POST("/services", s.handleCreateService)
	api.GET("/services/:name", s.handleGetService)
	api.POST("/services/:name/start", s.handleStartService)
	api.POST("/services/:name/stop", s.handleStopService)
	api.POST("/services/:name/restart", s.handleRestartService)
	api.GET("/services/:name/logs", s.handleGetLogs)
	api.POST("/services/:name/logs/clear", s.handleClearLogs)
	api.GET("/services/:name/config", s.handleGetConfig)
	api.PUT("/services/:name/config", s.handleUpdateConfig)
	api.POST("/save-config", s.handleSaveConfigToFile)
	api.DELETE("/services/:name", s.handleDeleteService)
	api.GET("/cron/:name/history", s.handleGetCronHistory)
	api.POST("/cron/validate", s.handleValidateCron)

	api.GET("/events", s.handleSSE)

	s.app.GET("/assets/*", staticAssetHandler)
	s.app.GET("/*", spaFallbackHandler)
}
