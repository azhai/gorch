package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/azhai/gorch/internal/config"
	"github.com/azhai/gorch/internal/cron"
	"github.com/azhai/gorch/internal/ipc"
	"github.com/azhai/gorch/internal/logs"
	"github.com/azhai/gorch/internal/status"
	"github.com/azhai/gorch/internal/web"
)

const maxRestartCount = 3

type Supervisor struct {
	cfg         *config.Config
	processes   map[string]*ProcessInfo
	mu          sync.RWMutex
	cronSched   *cron.Scheduler
	logMgr      *logs.Manager
	statusCache *status.Cache
	webServer   *web.Server
	ipcClosers  []func()
	hub         *web.Hub
	pidPath     string
	socketPath  string
	configPath  string
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

type Option func(*Supervisor)

func WithPidPath(path string) Option {
	return func(s *Supervisor) { s.pidPath = path }
}

func WithSocketPath(path string) Option {
	return func(s *Supervisor) { s.socketPath = path }
}

func WithConfigPath(path string) Option {
	return func(s *Supervisor) { s.configPath = path }
}

func NewSupervisor(cfg *config.Config, opts ...Option) *Supervisor {
	s := &Supervisor{
		cfg:         cfg,
		processes:   make(map[string]*ProcessInfo),
		cronSched:   cron.NewScheduler(),
		statusCache: status.NewCache(),
		pidPath:     "/tmp/gorch.pid",
		socketPath:  "/tmp/gorch.sock",
	}

	for _, opt := range opts {
		opt(s)
	}

	s.logMgr = logs.NewManager("/var/log/gorch")
	s.hub = web.NewHub()

	return s
}

func (s *Supervisor) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	defer cancel()

	if err := AcquireLock(s.pidPath + ".lock"); err != nil {
		return fmt.Errorf("another instance is running: %w", err)
	}

	if err := WritePidFile(s.pidPath, os.Getpid(), nil); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Clean up orphan processes from previous runs
	s.cleanupOrphans()

	order := s.cfg.TopologicalOrder()
	slog.Info("starting services", "order", order)

	for _, name := range order {
		svc := s.cfg.Services[name]
		if svc.CRON != "" {
			if err := s.registerCronJob(name, svc); err != nil {
				return fmt.Errorf("failed to register cron for '%s': %w", name, err)
			}
			s.statusCache.Update(name, status.ServiceStatus{
				Name:   name,
				Status: config.StatusStopped,
			})
			continue
		}

		if err := s.startService(ctx, name, svc); err != nil {
			slog.Error("failed to start service", "service", name, "error", err)
			return err
		}
	}

	s.cronSched.Start()

	if s.cfg.Web.WEB_ENABLE {
		s.webServer = web.NewServer(s.cfg.Web.WEB_ADDR, s)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.webServer.Start(); err != nil {
				slog.Error("web server error", "error", err)
			}
		}()
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		listener := ipc.NewListener(s)
		s.ipcClosers = append(s.ipcClosers, listener.Close)
		listener.Listen(s.socketPath)
	}()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.hub.Run()
	}()

	// uptime ticker: broadcast every 5 seconds aligned to clock
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.uptimeTicker(ctx)
	}()

	<-ctx.Done()
	return s.Stop(context.Background())
}

func (s *Supervisor) uptimeTicker(ctx context.Context) {
	// align to next 5-second boundary
	now := time.Now()
	next := now.Truncate(5 * time.Second).Add(5 * time.Second)
	select {
	case <-time.After(next.Sub(now)):
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// send initial tick
	s.hub.BroadcastUptimeTick(s.GetAllStatus())

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.hub.BroadcastUptimeTick(s.GetAllStatus())
		}
	}
}

func (s *Supervisor) Stop(ctx context.Context) error {
	slog.Info("stopping supervisor")

	s.cronSched.Stop()

	order := s.cfg.TopologicalOrder()
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		if err := s.stopService(name); err != nil {
			slog.Error("failed to stop service", "service", name, "error", err)
		}
	}

	if s.webServer != nil {
		s.webServer.Stop()
	}

	for _, closer := range s.ipcClosers {
		closer()
	}

	s.hub.Stop()
	ReleaseLock(s.pidPath + ".lock")
	os.Remove(s.pidPath)
	os.Remove(s.socketPath)

	// Clean up any remaining service PID files
	os.RemoveAll(ServicePidDir)

	s.wg.Wait()
	return nil
}

func (s *Supervisor) StartService(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	svc, exists := s.cfg.Services[name]
	if !exists {
		return fmt.Errorf("service not found: %s", name)
	}

	if svc.CRON != "" {
		return fmt.Errorf("cannot directly start cron service '%s', use reload", name)
	}

	return s.startService(ctx, name, svc)
}

func (s *Supervisor) StopService(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopService(name)
}

func (s *Supervisor) RestartService(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	svc, exists := s.cfg.Services[name]
	if !exists {
		return fmt.Errorf("service not found: %s", name)
	}

	if err := s.stopService(name); err != nil {
		slog.Warn("stop service failed during restart", "service", name, "error", err)
	}

	return s.startService(ctx, name, svc)
}

func (s *Supervisor) GetStatus(name string) (status.ServiceStatus, bool) {
	return s.statusCache.Get(name)
}

func (s *Supervisor) GetAllStatus() map[string]status.ServiceStatus {
	// Refresh memory usage for running processes
	s.mu.RLock()
	for name, proc := range s.processes {
		if proc.Cmd != nil && proc.Cmd.Process != nil {
			memMB := getProcessMemoryMB(proc.Pid)
			s.statusCache.UpdateMemory(name, memMB)
		}
	}
	s.mu.RUnlock()
	return s.statusCache.GetAll()
}

func (s *Supervisor) GetConfig() *config.Config {
	return s.cfg
}

func (s *Supervisor) GetLogManager() *logs.Manager {
	return s.logMgr
}

func (s *Supervisor) GetCronScheduler() *cron.Scheduler {
	return s.cronSched
}

func (s *Supervisor) GetHub() *web.Hub {
	return s.hub
}

func (s *Supervisor) UpdateServiceConfig(name string, svc config.ServiceConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.cfg.Services[name]; !exists {
		return fmt.Errorf("service not found: %s", name)
	}

	s.cfg.Services[name] = svc

	// 依赖关系可能变化，重算拓扑序
	s.cfg.RecalcTopoOrder()
	return nil
}

// CreateService adds a new service to the config.
func (s *Supervisor) CreateService(name string, svc config.ServiceConfig) error {
	s.mu.Lock()

	if _, exists := s.cfg.Services[name]; exists {
		s.mu.Unlock()
		return fmt.Errorf("service already exists: %s", name)
	}

	s.cfg.Services[name] = svc

	// 重算拓扑序，保证依赖启动顺序正确
	s.cfg.RecalcTopoOrder()

	// 写入 statusCache，否则 GetAllStatus 看不到新服务
	s.statusCache.Update(name, status.ServiceStatus{
		Name:   name,
		Status: config.StatusStopped,
	})
	s.mu.Unlock()

	// 注册 cron（如果有），需在锁外执行避免死锁
	if svc.CRON != "" {
		if err := s.registerCronJob(name, svc); err != nil {
			slog.Error("failed to register cron for new service", "service", name, "error", err)
		}
	}

	return nil
}

// DeleteService removes a service from the config.
// If the service is running, it will be stopped first.
func (s *Supervisor) DeleteService(name string) error {
	s.mu.Lock()
	if _, exists := s.cfg.Services[name]; !exists {
		s.mu.Unlock()
		return fmt.Errorf("service not found: %s", name)
	}

	proc, running := s.processes[name]
	s.mu.Unlock()

	if running {
		StopProcess(proc, 10*time.Second)
	}

	s.mu.Lock()
	delete(s.processes, name)
	delete(s.cfg.Services, name)
	s.statusCache.Update(name, status.ServiceStatus{Name: name, Status: config.StatusStopped})
	s.mu.Unlock()
	return nil
}

// SaveConfig persists the current config to the TOML file.
func (s *Supervisor) SaveConfig() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Save(s.configPath)
}

func (s *Supervisor) HandleCommand(cmd ipc.ControlCommand) ipc.ControlResponse {
	ctx := context.Background()

	switch cmd.Action {
	case "status":
		allStatus := s.GetAllStatus()
		if cmd.Service != nil {
			if st, ok := s.GetStatus(*cmd.Service); ok {
				return ipc.OkResponse(st)
			}
			return ipc.ErrorResponse(fmt.Sprintf("service not found: %s", *cmd.Service))
		}
		return ipc.OkResponse(allStatus)

	case "start":
		if cmd.Service == nil {
			return ipc.ErrorResponse("service name required")
		}
		if err := s.StartService(ctx, *cmd.Service); err != nil {
			return ipc.ErrorResponse(err.Error())
		}
		return ipc.OkResponse(map[string]string{"message": fmt.Sprintf("service %s started", *cmd.Service)})

	case "stop":
		if cmd.Service == nil {
			return ipc.ErrorResponse("service name required")
		}
		if err := s.StopService(ctx, *cmd.Service); err != nil {
			return ipc.ErrorResponse(err.Error())
		}
		return ipc.OkResponse(map[string]string{"message": fmt.Sprintf("service %s stopped", *cmd.Service)})

	case "restart":
		if cmd.Service == nil {
			return ipc.ErrorResponse("service name required")
		}
		if err := s.RestartService(ctx, *cmd.Service); err != nil {
			return ipc.ErrorResponse(err.Error())
		}
		return ipc.OkResponse(map[string]string{"message": fmt.Sprintf("service %s restarted", *cmd.Service)})

	case "shutdown":
		go func() {
			s.Stop(context.Background())
		}()
		return ipc.OkResponse(map[string]string{"message": "shutting down"})

	default:
		return ipc.ErrorResponse(fmt.Sprintf("unknown action: %s", cmd.Action))
	}
}

func (s *Supervisor) startService(ctx context.Context, name string, svc config.ServiceConfig) error {
	proc, err := StartProcess(ctx, svc, name)
	if err != nil {
		s.statusCache.Update(name, status.ServiceStatus{
			Name:   name,
			Status: config.StatusFailed,
		})
		s.hub.BroadcastStatusChange(name, string(config.StatusFailed), 0, 0, 0)
		return err
	}

	s.processes[name] = proc
	memMB := getProcessMemoryMB(proc.Pid)
	s.statusCache.Update(name, status.ServiceStatus{
		Name:      name,
		Status:    config.StatusRunning,
		Pid:       proc.Pid,
		StartedAt: proc.StartTime.Unix(),
		MemoryMB:  memMB,
	})
	s.hub.BroadcastStatusChange(name, string(config.StatusRunning), proc.Pid, 0, memMB)

	s.wg.Add(1)
	go s.monitorLoop(ctx, name, svc)

	return nil
}

func (s *Supervisor) stopService(name string) error {
	proc, exists := s.processes[name]
	if !exists {
		return fmt.Errorf("service is not running: %s", name)
	}

	if err := StopProcess(proc, 10*time.Second); err != nil {
		return err
	}

	proc.ManualStop = true

	delete(s.processes, name)
	s.statusCache.Update(name, status.ServiceStatus{
		Name:     name,
		Status:   config.StatusStopped,
		MemoryMB: 0,
	})
	s.hub.BroadcastStatusChange(name, string(config.StatusStopped), 0, 0, 0)

	return nil
}

func (s *Supervisor) monitorLoop(ctx context.Context, name string, svc config.ServiceConfig) {
	defer s.wg.Done()

	proc, exists := s.processes[name]
	if !exists {
		return
	}

	exitCh, err := MonitorProcess(proc)
	if err != nil {
		slog.Error("monitor setup failed", "service", name, "error", err)
		return
	}

	exitCode := <-exitCh
	slog.Info("service exited", "service", name, "exitCode", exitCode)

	// Check manual stop without holding the lock (set by stopService before we get here)
	if proc.ManualStop {
		return
	}

	s.mu.Lock()
	// Re-check: stopService may have set ManualStop while we waited for the lock
	if proc.ManualStop {
		s.mu.Unlock()
		return
	}

	shouldRestart := false
	switch config.RestartPolicy(svc.RESTART_POLICY) {
	case config.RestartAlways:
		shouldRestart = true
	case config.RestartOnFailure:
		shouldRestart = exitCode != 0
	case config.RestartNever:
		shouldRestart = false
	}

	if shouldRestart && proc.RestartCnt < maxRestartCount {
		proc.RestartCnt++
		// Remove old proc before starting new one
		delete(s.processes, name)
		s.mu.Unlock()

		if svc.BACK_OFF > 0 {
			slog.Info("backing off before restart", "service", name, "backOffSeconds", svc.BACK_OFF)
			time.Sleep(time.Duration(svc.BACK_OFF) * time.Second)
		}

		if err := s.startService(ctx, name, svc); err != nil {
			slog.Error("restart failed", "service", name, "error", err)
		}
		return
	}

	delete(s.processes, name)
	finalStatus := config.StatusStopped
	if exitCode != 0 {
		if proc.RestartCnt >= maxRestartCount {
			finalStatus = config.StatusCrashed
		} else {
			finalStatus = config.StatusFailed
		}
	}

	s.statusCache.Update(name, status.ServiceStatus{
		Name:     name,
		Status:   finalStatus,
		ExitCode: &exitCode,
	})
	s.hub.BroadcastStatusChange(name, string(finalStatus), 0, 0, 0)
	s.mu.Unlock()
}

func (s *Supervisor) registerCronJob(name string, svc config.ServiceConfig) error {
	return s.cronSched.AddJob(name, svc.CRON, "", func() {
		slog.Info("cron triggered", "service", name, "command", svc.EXEC_CMD, "cron_expr", svc.CRON)
		s.mu.Lock()
		defer s.mu.Unlock()

		if _, running := s.processes[name]; running {
			slog.Warn("cron overlap detected, skipping", "service", name)
			s.cronSched.RecordExecution(name, cron.CronExecutionRecord{
				Service:   name,
				StartedAt: time.Now(),
				Status:    "overlap",
			})
			return
		}

		ctx := context.Background()
		slog.Info("cron starting service", "service", name, "command", svc.EXEC_CMD)
		if err := s.startService(ctx, name, svc); err != nil {
			slog.Error("cron start failed", "service", name, "error", err, "command", svc.EXEC_CMD)
			s.cronSched.RecordExecution(name, cron.CronExecutionRecord{
				Service:   name,
				StartedAt: time.Now(),
				Status:    "failed",
			})
			return
		}

		proc := s.processes[name]
		slog.Info("cron service started", "service", name, "pid", proc.Pid)

		exitCh, _ := MonitorProcess(proc)
		exitCode := <-exitCh

		now := time.Now()
		record := cron.CronExecutionRecord{
			Service:   name,
			StartedAt: proc.StartTime,
			EndedAt:   &now,
			ExitCode:  &exitCode,
			Status:    "success",
			Pid:       proc.Pid,
		}
		if exitCode != 0 {
			record.Status = "failed"
			slog.Error("cron service exited with error", "service", name, "exitCode", exitCode, "pid", proc.Pid)
		} else {
			slog.Info("cron service completed successfully", "service", name, "exitCode", exitCode, "pid", proc.Pid)
		}
		s.cronSched.RecordExecution(name, record)

		delete(s.processes, name)
		s.statusCache.Update(name, status.ServiceStatus{
			Name:   name,
			Status: config.StatusStopped,
		})
	})
}

func (s *Supervisor) HandleReload() error {
	newCfg, err := config.Load(s.configPath)
	if err != nil {
		return fmt.Errorf("failed to reload config: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for name := range newCfg.Services {
		_, exists := s.cfg.Services[name]
		if !exists {
			slog.Info("new service detected", "service", name)
		} else {
			slog.Info("service config may have changed", "service", name)
		}
	}

	s.cfg = newCfg
	slog.Info("config reloaded")
	return nil
}

// cleanupOrphans checks for stale PID files from previous runs and kills orphan processes.
func (s *Supervisor) cleanupOrphans() {
	supervisorPid := os.Getpid()
	for name := range s.cfg.Services {
		if KillOrphanProcess(name, supervisorPid) {
			slog.Info("killed orphan process", "service", name)
		}
	}
}
