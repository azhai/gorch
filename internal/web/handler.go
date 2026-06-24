package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/azhai/gorch/internal/config"
	"github.com/labstack/echo/v4"
	"github.com/robfig/cron/v3"
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

func jsonResponse(c echo.Context, status int, resp APIResponse) error {
	return c.JSON(status, resp)
}

func (s *Server) handleGetServices(c echo.Context) error {
	allStatus := s.supervisor.GetAllStatus()
	return c.JSON(http.StatusOK, okResponse(allStatus))
}

func (s *Server) handleGetService(c echo.Context) error {
	name := strings.Clone(c.Param("name"))
	st, ok := s.supervisor.GetStatus(name)
	if !ok {
		return c.JSON(http.StatusNotFound, errResponse("service not found: "+name))
	}
	return c.JSON(http.StatusOK, okResponse(st))
}

func (s *Server) handleStartService(c echo.Context) error {
	name := strings.Clone(c.Param("name"))
	if err := s.supervisor.StartService(c.Request().Context(), name); err != nil {
		return c.JSON(http.StatusOK, errResponse(err.Error()))
	}
	return c.JSON(http.StatusOK, okResponse(map[string]string{"message": "service " + name + " started"}))
}

func (s *Server) handleStopService(c echo.Context) error {
	name := strings.Clone(c.Param("name"))
	if err := s.supervisor.StopService(c.Request().Context(), name); err != nil {
		return c.JSON(http.StatusOK, errResponse(err.Error()))
	}
	return c.JSON(http.StatusOK, okResponse(map[string]string{"message": "service " + name + " stopped"}))
}

func (s *Server) handleRestartService(c echo.Context) error {
	name := strings.Clone(c.Param("name"))
	if err := s.supervisor.RestartService(c.Request().Context(), name); err != nil {
		return c.JSON(http.StatusOK, errResponse(err.Error()))
	}
	return c.JSON(http.StatusOK, okResponse(map[string]string{"message": "service " + name + " restarted"}))
}

func (s *Server) handleGetLogs(c echo.Context) error {
	name := strings.Clone(c.Param("name"))
	lines := 500
	if l := c.QueryParam("lines"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			lines = v
		}
	}

	logType := c.QueryParam("type")
	if logType == "" {
		logType = "stdout"
	}

	cfg := s.supervisor.GetConfig()
	svc, exists := cfg.Services[name]
	if !exists {
		return c.JSON(http.StatusNotFound, errResponse("service not found: "+name))
	}

	logPath := svc.STDOUT
	if logType == "stderr" {
		logPath = svc.STDERR
	}
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
		return c.JSON(http.StatusOK, errResponse(err.Error()))
	}

	return c.JSON(http.StatusOK, okResponse(map[string]any{
		"lines":   logLines,
		"logPath": logPath,
	}))
}

func (s *Server) handleClearLogs(c echo.Context) error {
	name := strings.Clone(c.Param("name"))
	logType := c.QueryParam("type")
	if logType == "" {
		logType = "stdout"
	}

	cfg := s.supervisor.GetConfig()
	svc, exists := cfg.Services[name]
	if !exists {
		return c.JSON(http.StatusNotFound, errResponse("service not found: "+name))
	}

	logPath := svc.STDOUT
	if logType == "stderr" {
		logPath = svc.STDERR
	}
	if logPath == "" {
		logPath = svc.STDERR
	}

	logMgr := s.supervisor.GetLogManager()
	if err := logMgr.ClearFile(logPath); err != nil {
		return c.JSON(http.StatusOK, errResponse("clear failed: "+err.Error()))
	}

	return c.JSON(http.StatusOK, okResponse(map[string]string{"message": "logs cleared"}))
}

func (s *Server) handleGetConfig(c echo.Context) error {
	name := strings.Clone(c.Param("name"))
	cfg := s.supervisor.GetConfig()

	svc, exists := cfg.Services[name]
	if !exists {
		return c.JSON(http.StatusNotFound, errResponse("service not found: "+name))
	}

	return c.JSON(http.StatusOK, okResponse(svc))
}

func (s *Server) handleUpdateConfig(c echo.Context) error {
	name := strings.Clone(c.Param("name"))
	slog.Debug("update config", "service", name, "body_len", c.Request().ContentLength)

	cfg := s.supervisor.GetConfig()
	if _, exists := cfg.Services[name]; !exists {
		return c.JSON(http.StatusNotFound, errResponse("service not found: "+name))
	}

	var svc config.ServiceConfig
	if err := c.Bind(&svc); err != nil {
		return c.JSON(http.StatusBadRequest, errResponse("invalid request body: "+err.Error()))
	}

	slog.Debug("update config parsed", "service", name, "exec_cmd", svc.EXEC_CMD, "work_dir", svc.WORK_DIR)

	if svc.EXEC_CMD == "" {
		return c.JSON(http.StatusBadRequest, errResponse("EXEC_CMD is required"))
	}

	if !config.IsValidRestartPolicy(svc.RESTART_POLICY) {
		return c.JSON(http.StatusBadRequest, errResponse("invalid RESTART_POLICY: "+svc.RESTART_POLICY))
	}

	if svc.BACK_OFF < 0 {
		return c.JSON(http.StatusBadRequest, errResponse("BACK_OFF must be non-negative"))
	}

	for _, dep := range svc.DEPENDS_ON {
		if _, exists := cfg.Services[dep]; !exists {
			return c.JSON(http.StatusBadRequest, errResponse("unknown dependency: "+dep))
		}
	}

	if err := s.supervisor.UpdateServiceConfig(name, svc); err != nil {
		return c.JSON(http.StatusOK, errResponse(err.Error()))
	}

	return c.JSON(http.StatusOK, okResponse(map[string]string{"message": "config updated"}))
}

func (s *Server) handleSaveConfigToFile(c echo.Context) error {
	cfg := s.supervisor.GetConfig()
	names := make([]string, 0, len(cfg.Services))
	for n := range cfg.Services {
		names = append(names, n)
	}
	slog.Debug("save config to file", "services", names)
	if err := s.supervisor.SaveConfig(); err != nil {
		return c.JSON(http.StatusOK, errResponse("save failed: "+err.Error()))
	}
	return c.JSON(http.StatusOK, okResponse(map[string]string{"message": "config saved to file"}))
}

func (s *Server) handleGetCronHistory(c echo.Context) error {
	name := strings.Clone(c.Param("name"))
	sched := s.supervisor.GetCronScheduler()
	history := sched.GetHistory(name)
	return c.JSON(http.StatusOK, okResponse(history))
}

type createServiceRequest struct {
	Name string               `json:"name"`
	Svc  config.ServiceConfig `json:"svc"`
}

func (s *Server) handleCreateService(c echo.Context) error {
	var req createServiceRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errResponse("invalid request body: "+err.Error()))
	}

	if req.Name == "" {
		return c.JSON(http.StatusBadRequest, errResponse("service name is required"))
	}
	if req.Svc.EXEC_CMD == "" {
		return c.JSON(http.StatusBadRequest, errResponse("EXEC_CMD is required"))
	}

	if err := s.supervisor.CreateService(req.Name, req.Svc); err != nil {
		return c.JSON(http.StatusOK, errResponse(err.Error()))
	}

	if err := s.supervisor.SaveConfig(); err != nil {
		return c.JSON(http.StatusOK, errResponse("service created but save failed: "+err.Error()))
	}

	return c.JSON(http.StatusOK, okResponse(map[string]string{"message": "service " + req.Name + " created"}))
}

func (s *Server) handleDeleteService(c echo.Context) error {
	name := strings.Clone(c.Param("name"))

	if err := s.supervisor.DeleteService(name); err != nil {
		return c.JSON(http.StatusOK, errResponse(err.Error()))
	}

	if err := s.supervisor.SaveConfig(); err != nil {
		return c.JSON(http.StatusOK, errResponse("service deleted but save failed: "+err.Error()))
	}

	return c.JSON(http.StatusOK, okResponse(map[string]string{"message": "service " + name + " deleted"}))
}

type validateCronRequest struct {
	Expression string `json:"expression"`
}

func (s *Server) handleValidateCron(c echo.Context) error {
	var req validateCronRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errResponse("invalid request body: "+err.Error()))
	}

	if req.Expression == "" {
		return c.JSON(http.StatusOK, okResponse(map[string]any{
			"valid":   false,
			"message": "expression is required",
		}))
	}

	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	sched, err := parser.Parse(req.Expression)
	if err != nil {
		return c.JSON(http.StatusOK, okResponse(map[string]any{
			"valid":   false,
			"message": err.Error(),
		}))
	}

	now := time.Now()
	next := sched.Next(now)
	next2 := sched.Next(next)

	return c.JSON(http.StatusOK, okResponse(map[string]any{
		"valid":    true,
		"message":  "valid cron expression",
		"nextRun":  next.Format(time.RFC3339),
		"nextRun2": next2.Format(time.RFC3339),
	}))
}
