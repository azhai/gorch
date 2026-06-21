package web

import (
	"encoding/json"
	"strconv"

	"github.com/azhai/gorch/internal/config"
	"github.com/gofiber/fiber/v3"
)

type APIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func okResponse(data any) APIResponse {
	raw, _ := json.Marshal(data)
	return APIResponse{Success: true, Data: raw}
}

func errResponse(msg string) APIResponse {
	return APIResponse{Success: false, Message: msg}
}

func (s *Server) setupRoutes() {
	api := s.app.Group("/api")

	api.Post("/auth/login", func(c fiber.Ctx) error {
		return handleLogin(c, s.supervisor.GetConfig().Web)
	})

	api.Get("/services", s.handleGetServices)
	api.Get("/services/:name", s.handleGetService)
	api.Post("/services/:name/start", s.handleStartService)
	api.Post("/services/:name/stop", s.handleStopService)
	api.Post("/services/:name/restart", s.handleRestartService)
	api.Get("/services/:name/logs", s.handleGetLogs)
	api.Get("/services/:name/config", s.handleGetConfig)
	api.Put("/services/:name/config", s.handleUpdateConfig)
	api.Get("/cron/:name/history", s.handleGetCronHistory)
	api.Get("/ws", s.handleWebSocket)

	s.app.Get("/assets/*", staticAssetHandler)
	s.app.Get("/*", spaFallbackHandler)
}

func (s *Server) handleGetServices(c fiber.Ctx) error {
	allStatus := s.supervisor.GetAllStatus()
	return c.JSON(okResponse(allStatus))
}

func (s *Server) handleGetService(c fiber.Ctx) error {
	name := c.Params("name")
	st, ok := s.supervisor.GetStatus(name)
	if !ok {
		return c.Status(404).JSON(errResponse("service not found: " + name))
	}
	return c.JSON(okResponse(st))
}

func (s *Server) handleStartService(c fiber.Ctx) error {
	name := c.Params("name")
	if err := s.supervisor.StartService(c.Context(), name); err != nil {
		return c.JSON(errResponse(err.Error()))
	}
	return c.JSON(okResponse(map[string]string{"message": "service " + name + " started"}))
}

func (s *Server) handleStopService(c fiber.Ctx) error {
	name := c.Params("name")
	if err := s.supervisor.StopService(c.Context(), name); err != nil {
		return c.JSON(errResponse(err.Error()))
	}
	return c.JSON(okResponse(map[string]string{"message": "service " + name + " stopped"}))
}

func (s *Server) handleRestartService(c fiber.Ctx) error {
	name := c.Params("name")
	if err := s.supervisor.RestartService(c.Context(), name); err != nil {
		return c.JSON(errResponse(err.Error()))
	}
	return c.JSON(okResponse(map[string]string{"message": "service " + name + " restarted"}))
}

func (s *Server) handleGetLogs(c fiber.Ctx) error {
	name := c.Params("name")
	lines := 500
	if l := c.Query("lines"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			lines = v
		}
	}

	cfg := s.supervisor.GetConfig()
	svc, exists := cfg.Services[name]
	if !exists {
		return c.Status(404).JSON(errResponse("service not found: " + name))
	}

	logPath := svc.STDOUT
	if logPath == "" {
		logPath = svc.STDERR
	}

	logMgr := s.supervisor.GetLogManager()

	var logLines []string
	var err error

	if logPath != "" {
		logLines, err = logMgr.ReadFileLines(logPath, lines)
	} else {
		logLines, err = logMgr.ReadLogs(name, lines)
	}

	if err != nil {
		return c.JSON(errResponse(err.Error()))
	}

	return c.JSON(okResponse(map[string]any{
		"lines":   logLines,
		"logPath": logPath,
	}))
}

func (s *Server) handleGetConfig(c fiber.Ctx) error {
	name := c.Params("name")
	cfg := s.supervisor.GetConfig()

	svc, exists := cfg.Services[name]
	if !exists {
		return c.Status(404).JSON(errResponse("service not found: " + name))
	}

	return c.JSON(okResponse(svc))
}

func (s *Server) handleUpdateConfig(c fiber.Ctx) error {
	name := c.Params("name")

	cfg := s.supervisor.GetConfig()
	if _, exists := cfg.Services[name]; !exists {
		return c.Status(404).JSON(errResponse("service not found: " + name))
	}

	var svc config.ServiceConfig
	if err := c.Bind().Body(&svc); err != nil {
		return c.Status(400).JSON(errResponse("invalid request body: " + err.Error()))
	}

	if svc.EXEC_CMD == "" {
		return c.Status(400).JSON(errResponse("EXEC_CMD is required"))
	}

	validPolicies := map[string]bool{
		string(config.RestartAlways):    true,
		string(config.RestartOnFailure): true,
		string(config.RestartNever):     true,
	}
	if !validPolicies[svc.RESTART_POLICY] {
		return c.Status(400).JSON(errResponse("invalid RESTART_POLICY: " + svc.RESTART_POLICY))
	}

	if svc.BACK_OFF < 0 {
		return c.Status(400).JSON(errResponse("BACK_OFF must be non-negative"))
	}

	for _, dep := range svc.DEPENDS_ON {
		if _, exists := cfg.Services[dep]; !exists {
			return c.Status(400).JSON(errResponse("unknown dependency: " + dep))
		}
	}

	if err := s.supervisor.UpdateServiceConfig(name, svc); err != nil {
		return c.JSON(errResponse(err.Error()))
	}

	return c.JSON(okResponse(map[string]string{"message": "config updated"}))
}

func (s *Server) handleGetCronHistory(c fiber.Ctx) error {
	name := c.Params("name")
	sched := s.supervisor.GetCronScheduler()
	history := sched.GetHistory(name)
	return c.JSON(okResponse(history))
}
