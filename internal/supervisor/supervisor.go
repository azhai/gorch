package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"syscall"
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
	cfg              *config.Config
	processes        map[string]*ProcessInfo
	mu               sync.RWMutex
	cronSched        *cron.Scheduler
	logMgr           *logs.Manager
	statusCache      *status.Cache
	webServer        *web.Server
	ipcClosers       []func()
	hub              *web.Hub
	pidPath          string
	servicesLockPath string
	socketPath       string
	configPath       string
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	adoptCandidates  map[string]int // PIDs from lock file for adoption on startup
}

type Option func(*Supervisor)

func WithPidPath(path string) Option {
	return func(s *Supervisor) { s.pidPath = path }
}

func WithServicesLock(path string) Option {
	return func(s *Supervisor) { s.servicesLockPath = path }
}

func WithSocketPath(path string) Option {
	return func(s *Supervisor) { s.socketPath = path }
}

func WithConfigPath(path string) Option {
	return func(s *Supervisor) { s.configPath = path }
}

func NewSupervisor(cfg *config.Config, opts ...Option) *Supervisor {
	s := &Supervisor{
		cfg:              cfg,
		processes:        make(map[string]*ProcessInfo),
		cronSched:        cron.NewScheduler(),
		statusCache:      status.NewCache(),
		pidPath:          "/var/run/gorch.pid",
		servicesLockPath: "/var/run/gorch-services.lock",
		socketPath:       "/var/run/gorch.sock",
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

	// Read services lock file from previous graceful shutdown.
	// If fresh, adoptCandidates holds PIDs to adopt; if stale/missing, nil.
	s.adoptCandidates = s.readServicesLock()
	if s.adoptCandidates != nil {
		slog.Info("found fresh services lock, will try to adopt processes", "count", len(s.adoptCandidates))
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

	// Record running service PIDs to lock file before stopping.
	// On next startup, if the lock file is fresh, gorch will try to adopt
	// these processes instead of killing and restarting them.
	s.writeServicesLock()

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
	os.Remove(s.servicesLockPath)
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
	if ctx.Err() != nil {
		return fmt.Errorf("supervisor is shutting down: %w", ctx.Err())
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	svc, exists := s.cfg.Services[name]
	if !exists {
		return fmt.Errorf("service not found: %s", name)
	}

	// If RESTART_CMD is set, run it in WORK_DIR instead of stop+start.
	// This is for daemons (e.g. nginx/angie) that support graceful reload
	// via a signal command like "nginx -s reload".
	if svc.RESTART_CMD != "" {
		slog.Info("restarting service via RESTART_CMD", "service", name, "cmd", svc.RESTART_CMD)
		if err := runPreAction(svc.RESTART_CMD, svc.WORK_DIR); err != nil {
			s.statusCache.Update(name, status.ServiceStatus{
				Name:   name,
				Status: config.StatusFailed,
			})
			s.hub.BroadcastStatusChange(name, string(config.StatusFailed), 0, 0, 0)
			return fmt.Errorf("RESTART_CMD failed: %w", err)
		}
		// Refresh status from the real process (may have new PID after reload)
		if proc, ok := s.processes[name]; ok {
			var found int
			if svc.PID_FILE != "" {
				found = tryReadUserPidFile(svc.PID_FILE)
			} else {
				found = findMainProcessByName(svc.EXEC_CMD)
			}
			if found > 0 {
				proc.Pid = found
			}
			treeMB := getProcessTreeMemoryMB(proc.Pid)
			s.statusCache.Update(name, status.ServiceStatus{
				Name:      name,
				Status:    config.StatusRunning,
				Pid:       proc.Pid,
				StartedAt: proc.StartTime.Unix(),
				MemoryMB:  treeMB,
			})
			s.hub.BroadcastStatusChange(name, string(config.StatusRunning), proc.Pid, 0, treeMB)
		}
		return nil
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
		pid, info := s.resolveProcessInfo(name, proc)

		if info.state == "" {
			// Process is truly gone — mark as stopped.
			slog.Info("process not found, marking stopped", "service", name, "lastPid", proc.Pid)
			s.statusCache.Update(name, status.ServiceStatus{
				Name:     name,
				Status:   config.StatusStopped,
				MemoryMB: 0,
			})
			s.hub.BroadcastStatusChange(name, string(config.StatusStopped), 0, 0, 0)
			continue
		}
		// Update proc.Pid if we found a new one (daemonize: original exited, new master running)
		pidChanged := pid != proc.Pid
		if pidChanged {
			slog.Info("PID changed, updating", "service", name, "oldPid", proc.Pid, "newPid", pid)
			proc.Pid = pid
			proc.StartTime = time.Now()
		}
		slog.Debug("process info", "service", name, "pid", pid, "ppid", info.ppid, "state", info.state, "etime", info.etime, "rssMB", info.rssMB, "cmd", info.command)
		// Use getProcessTreeMemoryMB for memory, but if that fails (e.g. ps can't see the process),
		// fall back to reading /proc/<pid>/statm directly (Linux only).
		treeMB := getProcessTreeMemoryMB(pid)
		if treeMB == 0 && info.state != "" {
			// If memory is 0 but process is running, try to read memory directly.
			if mem := readProcMemory(pid); mem > 0 {
				treeMB = mem
			}
		}
		if pidChanged {
			// PID changed — update cache with new PID and reset uptime
			s.statusCache.UpdateProcessInfo(name, pid, treeMB)
		} else {
			s.statusCache.UpdateMemory(name, treeMB)
		}
	}
	s.mu.RUnlock()
	return s.statusCache.GetAll()
}

// resolveProcessInfo finds the live PID and process info for a tracked service.
// It handles daemonized processes by checking PID_FILE or searching by name.
func (s *Supervisor) resolveProcessInfo(name string, proc *ProcessInfo) (int, psInfo) {
	pid := proc.Pid
	info := getProcessInfo(pid)
	if info.state != "" {
		return pid, info
	}

	slog.Info("GetAllStatus: process not found via ps", "service", name, "pid", pid, "proc.Pid", proc.Pid, "proc.StartTime", proc.StartTime)

	svc, ok := s.cfg.Services[name]
	if !ok {
		return pid, info
	}

	// If PID_FILE is configured, read it; otherwise fall back to findMainProcessByName.
	if svc.PID_FILE != "" {
		if found := tryReadUserPidFile(svc.PID_FILE); found > 0 {
			// With PID_FILE, trust the PID file and verify with kill -0.
			// This handles cases where ps can't see the process (e.g. PPID=1, permissions).
			if syscall.Kill(found, 0) == nil {
				info.state = "R" // assume running
				slog.Info("process found via PID_FILE + kill -0", "service", name, "pid", found)
				return found, info
			}
			info = getProcessInfo(found) // still try ps
			if info.state != "" {
				return found, info
			}
		}
	}

	if found := findMainProcessByName(svc.EXEC_CMD); found > 0 {
		info = getProcessInfo(found)
		if info.state != "" {
			return found, info
		}
	}

	return pid, info
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
	// Try to adopt a previously-running process via lock file candidates.
	// This happens when gorch was gracefully restarted and services survived.
	if s.adoptCandidates != nil {
		if pid, ok := s.adoptCandidates[name]; ok && pid > 0 {
			if proc := AdoptProcess(name, pid); proc != nil {
				slog.Info("adopted running process", "service", name, "pid", pid)
				s.processes[name] = proc
				treeMB := getProcessTreeMemoryMB(proc.Pid)
				s.statusCache.Update(name, status.ServiceStatus{
					Name:      name,
					Status:    config.StatusRunning,
					Pid:       proc.Pid,
					StartedAt: proc.StartTime.Unix(),
					MemoryMB:  treeMB,
				})
				s.hub.BroadcastStatusChange(name, string(config.StatusRunning), proc.Pid, 0, treeMB)
				s.wg.Add(1)
				go s.monitorAdoptedLoop(ctx, name, svc)
				// Write per-service PID file for consistency
				WriteServicePidFile(name, proc.Pid)
				return nil
			}
			// Process is dead, remove from candidates
			delete(s.adoptCandidates, name)
		}
	}

	if svc.PRE_ACTION != "" {
		if err := runPreAction(svc.PRE_ACTION, svc.WORK_DIR); err != nil {
			slog.Warn("pre-action failed", "service", name, "cmd", svc.PRE_ACTION, "error", err)
		}
	}

	if svc.CHECK_PORT > 0 {
		killPortProcess(svc.CHECK_PORT)
	}

	proc, err := StartProcess(ctx, svc, name)
	if err != nil {
		s.statusCache.Update(name, status.ServiceStatus{
			Name:   name,
			Status: config.StatusFailed,
		})
		s.hub.BroadcastStatusChange(name, string(config.StatusFailed), 0, 0, 0)
		return err
	}

	// Wait briefly for daemonized processes (e.g. nginx/angie) that fork a new
	// master and exit the original PID. If the original PID dies but a process
	// with the same executable name appears, switch to tracking the new master.
	if found := s.detectDaemonize(proc, svc); found > 0 {
		proc.Pid = found
		proc.Adopted = true
		// Close the Cmd handle — the original process is gone, Wait() would fail.
		if proc.Cmd != nil && proc.Cmd.Process != nil {
			proc.Cmd.Process.Release()
		}
		proc.Cmd = nil
		WriteServicePidFile(name, found)
	}

	s.processes[name] = proc
	treeMB := getProcessTreeMemoryMB(proc.Pid)
	s.statusCache.Update(name, status.ServiceStatus{
		Name:      name,
		Status:    config.StatusRunning,
		Pid:       proc.Pid,
		StartedAt: proc.StartTime.Unix(),
		MemoryMB:  treeMB,
	})
	s.hub.BroadcastStatusChange(name, string(config.StatusRunning), proc.Pid, 0, treeMB)

	if proc.Adopted {
		s.wg.Add(1)
		go s.monitorAdoptedLoop(ctx, name, svc)
	} else {
		s.wg.Add(1)
		go s.monitorLoop(ctx, name, svc)
	}

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

// cleanupOrphans removes stale PID files for processes that are no longer running.
// Live processes are adopted by startService, not killed here.
func (s *Supervisor) cleanupOrphans() {
	for name := range s.cfg.Services {
		pid, err := ReadServicePidFile(name)
		if err != nil || pid <= 0 {
			continue
		}
		p, err := os.FindProcess(pid)
		if err != nil {
			RemoveServicePidFile(name)
			continue
		}
		if err := p.Signal(syscall.Signal(0)); err != nil {
			// process is dead, clean up stale PID file
			RemoveServicePidFile(name)
			slog.Info("cleaned stale pid file", "service", name, "pid", pid)
		}
	}
}

// writeServicesLock records all running service PIDs to the lock file.
// Called on graceful shutdown so the next startup can adopt live processes.
func (s *Supervisor) writeServicesLock() {
	s.mu.RLock()
	services := make(map[string]int)
	for name, proc := range s.processes {
		if proc.Pid > 0 {
			services[name] = proc.Pid
		}
	}
	s.mu.RUnlock()

	if len(services) == 0 {
		return
	}

	if err := WritePidFile(s.servicesLockPath, os.Getpid(), services); err != nil {
		slog.Warn("failed to write services lock file", "error", err)
	}
}

// readServicesLock reads the lock file and returns service PIDs if fresh.
// Returns nil if the file is missing, stale, or invalid.
func (s *Supervisor) readServicesLock() map[string]int {
	pidFile, err := ReadPidFile(s.servicesLockPath)
	if err != nil {
		return nil
	}
	if time.Since(pidFile.UpdatedAt) > maxOrphanAge {
		slog.Info("services lock file is stale, ignoring", "age", time.Since(pidFile.UpdatedAt))
		os.Remove(s.servicesLockPath)
		return nil
	}
	return pidFile.Services
}
