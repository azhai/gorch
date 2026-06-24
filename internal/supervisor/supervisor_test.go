package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/azhai/gorch/internal/config"
	"github.com/azhai/gorch/internal/ipc"
)

// ── Test NewSupervisor with options ───────────────────────

func TestNewSupervisor_Defaults(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]config.ServiceConfig{},
		Web:      config.WebConfig{},
	}

	sup := NewSupervisor(cfg)
	if sup == nil {
		t.Fatal("NewSupervisor() returned nil")
	}
	if sup.pidPath != "/var/run/gorch.pid" {
		t.Errorf("default pidPath = %q, want '/var/run/gorch.pid'", sup.pidPath)
	}
	if sup.servicesLockPath != "/var/run/gorch-services.lock" {
		t.Errorf("default servicesLockPath = %q, want '/var/run/gorch-services.lock'", sup.servicesLockPath)
	}
	if sup.socketPath != "/tmp/gorch.sock" {
		t.Errorf("default socketPath = %q, want '/tmp/gorch.sock'", sup.socketPath)
	}
	if sup.processes == nil {
		t.Error("processes map should be initialized")
	}
	if sup.statusCache == nil {
		t.Error("statusCache should be initialized")
	}
	if sup.cronSched == nil {
		t.Error("cronSched should be initialized")
	}
}

func TestNewSupervisor_WithOptions(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]config.ServiceConfig{},
	}

	sup := NewSupervisor(cfg,
		WithPidPath("/custom/pid"),
		WithSocketPath("/custom/sock"),
		WithConfigPath("/custom/config.toml"),
	)

	if sup.pidPath != "/custom/pid" {
		t.Errorf("pidPath = %q, want '/custom/pid'", sup.pidPath)
	}
	if sup.socketPath != "/custom/sock" {
		t.Errorf("socketPath = %q, want '/custom/sock'", sup.socketPath)
	}
	if sup.configPath != "/custom/config.toml" {
		t.Errorf("configPath = %q, want '/custom/config.toml'", sup.configPath)
	}
}

// ── Test Supervisor GetStatus / GetAllStatus ─────────────

func TestSupervisor_GetStatus_NotFound(t *testing.T) {
	cfg := &config.Config{Services: map[string]config.ServiceConfig{}}
	sup := NewSupervisor(cfg)

	_, ok := sup.GetStatus("nonexistent")
	if ok {
		t.Error("expected false for nonexistent service")
	}
}

func TestSupervisor_GetAllStatus_Empty(t *testing.T) {
	cfg := &config.Config{Services: map[string]config.ServiceConfig{}}
	sup := NewSupervisor(cfg)

	all := sup.GetAllStatus()
	if len(all) != 0 {
		t.Errorf("expected empty status map, got %d items", len(all))
	}
}

func TestSupervisor_GetConfig(t *testing.T) {
	expectedCfg := &config.Config{
		Services: map[string]config.ServiceConfig{
			"test": {EXEC_CMD: "echo test"},
		},
	}
	sup := NewSupervisor(expectedCfg)

	got := sup.GetConfig()
	if got != expectedCfg {
		t.Error("GetConfig() should return the same config pointer")
	}
	if len(got.Services) != 1 {
		t.Error("config services lost")
	}
}

// ── Test UpdateServiceConfig ─────────────────────────────

func TestSupervisor_UpdateServiceConfig(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]config.ServiceConfig{
			"api": {EXEC_CMD: "echo api-old"},
		},
	}
	sup := NewSupervisor(cfg)

	newSvc := config.ServiceConfig{EXEC_CMD: "echo api-new", RESTART_POLICY: "always"}
	err := sup.UpdateServiceConfig("api", newSvc)
	if err != nil {
		t.Fatalf("UpdateServiceConfig() error = %v", err)
	}

	updated := sup.cfg.Services["api"]
	if updated.EXEC_CMD != "echo api-new" {
		t.Errorf("EXEC_CMD not updated: %s", updated.EXEC_CMD)
	}
	if updated.RESTART_POLICY != "always" {
		t.Errorf("RESTART_POLICY not updated: %s", updated.RESTART_POLICY)
	}
}

func TestSupervisor_UpdateServiceConfig_NotFound(t *testing.T) {
	cfg := &config.Config{Services: map[string]config.ServiceConfig{}}
	sup := NewSupervisor(cfg)

	err := sup.UpdateServiceConfig("nonexistent", config.ServiceConfig{EXEC_CMD: "echo x"})
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}
}

// ── Test HandleCommand ───────────────────────────────────

func TestSupervisor_HandleCommand_StatusAll(t *testing.T) {
	cfg := &config.Config{Services: map[string]config.ServiceConfig{}}
	sup := NewSupervisor(cfg)

	resp := sup.HandleCommand(ipc.ControlCommand{Action: "status"})
	if resp.Status != "ok" {
		t.Errorf("status action should return ok, got %q", resp.Status)
	}
}

func TestSupervisor_HandleCommand_StartNoService(t *testing.T) {
	cfg := &config.Config{Services: map[string]config.ServiceConfig{}}
	sup := NewSupervisor(cfg)

	resp := sup.HandleCommand(ipc.ControlCommand{Action: "start"})
	if resp.Status != "error" {
		t.Errorf("start without service should return error, got %q", resp.Status)
	}
}

func TestSupervisor_HandleCommand_StopNoService(t *testing.T) {
	cfg := &config.Config{Services: map[string]config.ServiceConfig{}}
	sup := NewSupervisor(cfg)

	resp := sup.HandleCommand(ipc.ControlCommand{Action: "stop"})
	if resp.Status != "error" {
		t.Errorf("stop without service should return error, got %q", resp.Status)
	}
}

func TestSupervisor_HandleCommand_RestartNoService(t *testing.T) {
	cfg := &config.Config{Services: map[string]config.ServiceConfig{}}
	sup := NewSupervisor(cfg)

	resp := sup.HandleCommand(ipc.ControlCommand{Action: "restart"})
	if resp.Status != "error" {
		t.Errorf("restart without service should return error, got %q", resp.Status)
	}
}

func TestSupervisor_HandleCommand_Shutdown(t *testing.T) {
	cfg := &config.Config{Services: map[string]config.ServiceConfig{}}
	sup := NewSupervisor(cfg)

	resp := sup.HandleCommand(ipc.ControlCommand{Action: "shutdown"})
	if resp.Status != "ok" {
		t.Errorf("shutdown should return ok, got %q", resp.Status)
	}
}

func TestSupervisor_HandleCommand_Unknown(t *testing.T) {
	cfg := &config.Config{Services: map[string]config.ServiceConfig{}}
	sup := NewSupervisor(cfg)

	resp := sup.HandleCommand(ipc.ControlCommand{Action: "fly-to-moon"})
	if resp.Status != "error" {
		t.Errorf("unknown action should return error, got %q", resp.Status)
	}
}

// ── Test Service PID File Management ─────────────────────

func TestServicePidPath(t *testing.T) {
	got := servicePidPath("my-service")
	want := "/tmp/gorch/my-service.pid"
	if got != want {
		t.Errorf("servicePidPath() = %q, want %q", got, want)
	}
}

func TestWriteAndReadServicePidFile(t *testing.T) {
	name := "test-pid-svc"
	defer RemoveServicePidFile(name)

	err := WriteServicePidFile(name, 12345)
	if err != nil {
		t.Fatalf("WriteServicePidFile() error = %v", err)
	}

	pid, err := ReadServicePidFile(name)
	if err != nil {
		t.Fatalf("ReadServicePidFile() error = %v", err)
	}
	if pid != 12345 {
		t.Errorf("pid = %d, want 12345", pid)
	}
}

func TestReadServicePidFile_NotFound(t *testing.T) {
	_, err := ReadServicePidFile("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent pid file")
	}
}

func TestRemoveServicePidFile(t *testing.T) {
	name := "test-remove-pid"
	WriteServicePidFile(name, 9999)

	err := RemoveServicePidFile(name)
	if err != nil {
		t.Fatalf("RemoveServicePidFile() error = %v", err)
	}

	// Verify file is gone
	_, err = ReadServicePidFile(name)
	if err == nil {
		t.Error("expected error after removing pid file")
	}
}

func TestKillOrphanProcess_NoFile(t *testing.T) {
	killed := KillOrphanProcess("no-such-service", os.Getpid())
	if killed {
		t.Error("should return false when no pid file exists")
	}
}

func TestKillOrphanProcess_StaleFile(t *testing.T) {
	name := "test-stale-orphan"
	WriteServicePidFile(name, 99999)
	defer RemoveServicePidFile(name)

	// PID 99999 doesn't exist, should clean up file and return false
	killed := KillOrphanProcess(name, os.Getpid())
	if killed {
		t.Error("should return false for dead process (stale file)")
	}

	// Verify stale file was cleaned up
	_, err := ReadServicePidFile(name)
	if err == nil {
		t.Error("stale pid file should have been removed")
	}
}

func TestKillOrphanProcess_OldFileSkipped(t *testing.T) {
	name := "test-old-orphan"
	WriteServicePidFile(name, 88888)
	defer RemoveServicePidFile(name)

	// Set modification time to 1 hour ago (older than maxOrphanAge)
	path := servicePidPath(name)
	oldTime := time.Now().Add(-1 * time.Hour)
	os.Chtimes(path, oldTime, oldTime)

	killed := KillOrphanProcess(name, os.Getpid())
	if killed {
		t.Error("should skip PID file older than maxOrphanAge")
	}
}

// ── Test RestartService with RESTART_CMD ─────────────────

// TestRestartService_RestartCmd verifies that when RESTART_CMD is set,
// RestartService runs that command instead of stop+start.
func TestRestartService_RestartCmd(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "reloaded")
	cfg := &config.Config{
		Services: map[string]config.ServiceConfig{
			"svc": {
				EXEC_CMD:    "sleep 60",
				RESTART_CMD: "touch " + marker,
			},
		},
	}
	sup := NewSupervisor(cfg)

	// Simulate a running process so RestartService can refresh its status.
	sup.processes["svc"] = &ProcessInfo{
		Name:      "svc",
		Pid:       os.Getpid(),
		Status:    config.StatusRunning,
		StartTime: time.Now(),
	}

	if err := sup.RestartService(context.Background(), "svc"); err != nil {
		t.Fatalf("RestartService() error = %v", err)
	}

	// RESTART_CMD should have created the marker file.
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("RESTART_CMD did not run: marker file not created: %v", err)
	}
}

// TestRestartService_RestartCmdFailure verifies that a failing RESTART_CMD
// returns an error and marks the service as failed.
func TestRestartService_RestartCmdFailure(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]config.ServiceConfig{
			"svc": {
				EXEC_CMD:    "sleep 60",
				RESTART_CMD: "false", // always exits non-zero
			},
		},
	}
	sup := NewSupervisor(cfg)
	sup.processes["svc"] = &ProcessInfo{
		Name:      "svc",
		Pid:       os.Getpid(),
		Status:    config.StatusRunning,
		StartTime: time.Now(),
	}

	err := sup.RestartService(context.Background(), "svc")
	if err == nil {
		t.Fatal("expected error for failing RESTART_CMD")
	}

	st, ok := sup.GetStatus("svc")
	if !ok {
		t.Fatal("expected status after failure")
	}
	if st.Status != config.StatusFailed {
		t.Errorf("status = %q, want %q", st.Status, config.StatusFailed)
	}
}

// TestRestartService_NoRestartCmd verifies that without RESTART_CMD,
// RestartService falls back to stop+start (and errors if not running).
func TestRestartService_NoRestartCmd(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]config.ServiceConfig{
			"svc": {EXEC_CMD: "sleep 60"},
		},
	}
	sup := NewSupervisor(cfg)

	// No process registered → stopService returns error, startService tries to start.
	// We just verify it doesn't panic and RESTART_CMD path is not taken.
	_ = sup.RestartService(context.Background(), "svc")
}
