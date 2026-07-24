package web

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"strings"

	gototp "github.com/azhai/go-totp"
	"github.com/azhai/gobus/log"
	"github.com/azhai/gorch/config"
	"github.com/azhai/gorch/cron"
	"github.com/azhai/gorch/status"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

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

type Server struct {
	app        *echo.Echo
	supervisor SupervisorProvider
	addr       string
	TOTP       *gototp.TOTP
	urlPrefix  string // normalized: no leading/trailing slash, e.g. "gorch"
	indexHTML  []byte // pre-rendered with window.__URL_PREFIX__ injected
}

func NewServer(addr string, sup SupervisorProvider) *Server {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	cfg := sup.GetConfig().Web
	urlPrefix := normalizePrefix(cfg.URL_PREFIX)

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "OPTIONS", "DELETE"},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: false,
	}))

	e.Use(authMiddleware(cfg, urlPrefix))

	s := &Server{
		app:        e,
		supervisor: sup,
		addr:       addr,
		urlPrefix:  urlPrefix,
	}

	s.TOTP = gototp.NewTOTPFromOptions(gototp.Options{
		Enable: cfg.TOTP_ENABLE,
		Secret: cfg.TOTP_SECRET,
		DBPath: cfg.TOTP_DB,
		Issuer: "Gorch",
	})
	s.initIndexHTML()
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
	prefix := ""
	if s.urlPrefix != "" {
		prefix = "/" + s.urlPrefix
	}

	api := s.app.Group(prefix + "/api")

	api.POST("/auth/login", s.handleLogin)
	api.POST("/auth/login/totp", s.handleLoginTotp)

	if s.TOTP != nil {
		api.POST("/totp/setup", s.handleTOTPSetup)
		api.POST("/totp/verify-setup", s.handleTOTPVerifySetup)
		api.POST("/totp/verify", s.handleTOTPVerify)
		api.POST("/totp/verify-backup", s.handleTOTPVerifyBackup)
		api.POST("/totp/disable", s.handleTOTPDisable)
		api.GET("/totp/status", s.handleTOTPStatus)
		api.POST("/totp/regenerate-backup", s.handleTOTPRegenerateBackup)
	} else {
		api.Any("/totp/*", func(c echo.Context) error {
			return c.JSON(503, map[string]any{"success": false, "message": "TOTP not configured"})
		})
	}

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

	s.app.GET(prefix+"/assets/*", staticAssetHandler)
	s.app.GET("/*", s.spaFallbackHandler)
}

// initIndexHTML reads the embedded index.html and injects
// window.__URL_PREFIX__ so the SPA knows its mount point at runtime.
// Called once at server startup; the result is cached in s.indexHTML.
func (s *Server) initIndexHTML() {
	fsys := getFileSystem()
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		s.indexHTML = []byte("<!DOCTYPE html><html><body>web UI not found</body></html>")
		return
	}
	prefix := ""
	if s.urlPrefix != "" {
		prefix = "/" + s.urlPrefix
	}
	inject := fmt.Sprintf(`<script>window.__URL_PREFIX__="%s";</script>`, prefix)
	s.indexHTML = bytes.Replace(data, []byte("</head>"), []byte(inject+"</head>"), 1)
}

// normalizePrefix strips leading/trailing slashes from URL_PREFIX.
// "/gorch/" -> "gorch", "gorch" -> "gorch", "/" -> "", "" -> ""
func normalizePrefix(p string) string {
	return strings.Trim(strings.TrimSpace(p), "/")
}
