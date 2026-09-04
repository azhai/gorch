package supervisor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azhai/gorch/config"
	"github.com/azhai/gorch/status"
)

// ── PID file (daemon.go) ────────────────────────────────────

func TestWritePidFile_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gorch.pid")

	if err := WritePidFile(path, 4242, map[string]int{"api": 100, "redis": 200}); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	pf, err := ReadPidFile(path)
	if err != nil {
		t.Fatalf("ReadPidFile: %v", err)
	}
	if pf.SupervisorPid != 4242 {
		t.Errorf("SupervisorPid = %d, want 4242", pf.SupervisorPid)
	}
	if pf.Services["api"] != 100 || pf.Services["redis"] != 200 {
		t.Errorf("Services = %v, want api=100 redis=200", pf.Services)
	}
	if pf.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

func TestReadPidFile_Missing(t *testing.T) {
	if _, err := ReadPidFile(filepath.Join(t.TempDir(), "nope.pid")); err == nil {
		t.Error("expected an error for a missing PID file")
	}
}

func TestReadPidFile_InvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pid")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPidFile(path); err == nil {
		t.Error("expected an error for malformed JSON")
	}
}

// ── AdoptProcess ────────────────────────────────────────────

func TestAdoptProcess_LiveProcess(t *testing.T) {
	proc := AdoptProcess("self", os.Getpid())
	if proc == nil {
		t.Fatal("AdoptProcess returned nil for the current process")
	}
	if proc.Pid != os.Getpid() {
		t.Errorf("Pid = %d, want %d", proc.Pid, os.Getpid())
	}
	if !proc.Adopted {
		t.Error("Adopted should be true")
	}
	if proc.Status != config.StatusRunning {
		t.Errorf("Status = %q, want running", proc.Status)
	}
}

func TestAdoptProcess_DeadProcess(t *testing.T) {
	if proc := AdoptProcess("ghost", 999999); proc != nil {
		t.Errorf("AdoptProcess returned %+v for a non-existent PID, want nil", proc)
	}
}

// ── StartProcess ────────────────────────────────────────────

func TestStartProcess_EmptyExecCmd(t *testing.T) {
	_, err := StartProcess(context.Background(), config.ServiceConfig{WORK_DIR: t.TempDir(), EXEC_CMD: "   "}, "blank")
	if err == nil {
		t.Error("expected an error for an empty EXEC_CMD")
	}
}

func TestStartProcess_UnknownCommand(t *testing.T) {
	_, err := StartProcess(context.Background(),
		config.ServiceConfig{WORK_DIR: t.TempDir(), EXEC_CMD: "definitely-not-a-real-command"}, "nope")
	if err == nil {
		t.Error("expected an error for a command that cannot be started")
	}
}

func TestStartProcess_BadLogPath(t *testing.T) {
	_, err := StartProcess(context.Background(),
		config.ServiceConfig{
			WORK_DIR: t.TempDir(),
			EXEC_CMD: "true",
			STDOUT:   filepath.Join("/definitely/not/here", "out.log"),
		}, "badlog")
	if err == nil {
		t.Error("expected an error when the stdout log cannot be opened")
	}
}

// TestStartProcess_EnvAndLogFiles covers ENV_VARS plus separate stdout/stderr files.
func TestStartProcess_EnvAndLogFiles(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.log")
	errPath := filepath.Join(dir, "err.log")

	svc := config.ServiceConfig{
		WORK_DIR: dir,
		EXEC_CMD: "env", // prints the environment; no shell quoting needed
		ENV_VARS: map[string]string{"GREETING": "hello-gorch"},
		STDOUT:   outPath,
		STDERR:   errPath,
	}

	proc, err := StartProcess(context.Background(), svc, "env-svc")
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	defer RemoveServicePidFile("env-svc")

	// Reap the process so the log is flushed before we read it.
	exitCh, err := MonitorProcess(proc)
	if err != nil {
		t.Fatalf("MonitorProcess: %v", err)
	}
	select {
	case <-exitCh:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit")
	}
	proc.CloseFiles()

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	if !strings.Contains(string(data), "GREETING=hello-gorch") {
		t.Errorf("stdout log missing the injected env var:\n%s", data)
	}
}

// TestStartProcess_SharedStdoutStderr covers the branch where STDERR == STDOUT
// so both streams share one file handle.
func TestStartProcess_SharedStdoutStderr(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "combined.log")

	svc := config.ServiceConfig{
		WORK_DIR: dir,
		EXEC_CMD: "true",
		STDOUT:   logPath,
		STDERR:   logPath, // same path → shared handle
	}

	proc, err := StartProcess(context.Background(), svc, "shared-log")
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	defer RemoveServicePidFile("shared-log")

	if proc.StdoutFile != proc.StderrFile {
		t.Error("stdout and stderr should share one file when the paths are equal")
	}
	proc.CloseFiles()
}

func TestCloseFiles_NilSafe(t *testing.T) {
	// Must not panic when there are no open files.
	(&ProcessInfo{Name: "empty"}).CloseFiles()
}

// ── StopProcess ─────────────────────────────────────────────

func TestStopProcess_StartedProcess(t *testing.T) {
	name := "stop-started"
	svc := config.ServiceConfig{WORK_DIR: t.TempDir(), EXEC_CMD: "sleep 60"}

	proc, err := StartProcess(context.Background(), svc, name)
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}

	exitCh, err := MonitorProcess(proc)
	if err != nil {
		t.Fatalf("MonitorProcess: %v", err)
	}

	if err := StopProcess(proc, 3*time.Second); err != nil {
		t.Fatalf("StopProcess: %v", err)
	}

	select {
	case <-exitCh:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after StopProcess")
	}

	// The PID file must be cleaned up so a later run is not treated as an orphan.
	if _, err := ReadServicePidFile(name); err == nil {
		t.Error("PID file should have been removed by StopProcess")
	}
}

// TestStopProcess_Adopted covers the branch where there is no Cmd (a process
// adopted from a previous run) and the PID must be signalled directly.
func TestStopProcess_Adopted(t *testing.T) {
	name := "stop-adopted"
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	pid := cmd.Process.Pid

	// Give it a PID file so we can assert StopProcess removes it.
	if err := WriteServicePidFile(name, pid); err != nil {
		t.Fatal(err)
	}

	proc := &ProcessInfo{Name: name, Pid: pid, Adopted: true, StartTime: time.Now()}
	if err := StopProcess(proc, 5*time.Second); err != nil {
		t.Fatalf("StopProcess: %v", err)
	}
	_, _ = cmd.Process.Wait() // reap

	if _, err := ReadServicePidFile(name); err == nil {
		t.Error("PID file should have been removed for an adopted process")
	}
}

// StopProcess on a bare ProcessInfo (no Cmd, not adopted) must be a safe no-op.
func TestStopProcess_NoCmdNotAdopted(t *testing.T) {
	proc := &ProcessInfo{Name: "bare", Pid: 12345}
	if err := StopProcess(proc, time.Second); err != nil {
		t.Errorf("StopProcess: %v", err)
	}
}

// ── KillOrphanProcess ───────────────────────────────────────

func TestKillOrphanProcess_LiveProcess(t *testing.T) {
	name := "live-orphan"
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	if err := WriteServicePidFile(name, cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	defer RemoveServicePidFile(name)

	if !KillOrphanProcess(name, os.Getpid()) {
		t.Error("expected KillOrphanProcess to report killing a live orphan")
	}
}

// ── runPreAction ────────────────────────────────────────────

func TestRunPreAction_Success(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")

	if err := runPreAction("touch "+marker, dir); err != nil {
		t.Fatalf("runPreAction: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("PRE_ACTION did not create the marker: %v", err)
	}
}

func TestRunPreAction_Failure(t *testing.T) {
	if err := runPreAction("false", t.TempDir()); err == nil {
		t.Error("expected an error for a failing PRE_ACTION")
	}
}

func TestRunPreAction_Empty(t *testing.T) {
	if err := runPreAction("", t.TempDir()); err != nil {
		t.Errorf("empty PRE_ACTION should be a no-op, got %v", err)
	}
}

// ── process inspection helpers ──────────────────────────────

func TestGetProcessInfo_LiveProcess(t *testing.T) {
	info := getProcessInfo(os.Getpid())
	if info.state == "" {
		t.Error("expected a non-empty state for the current process")
	}
}

func TestGetProcessInfo_DeadProcess(t *testing.T) {
	info := getProcessInfo(999999)
	if info.state != "" {
		t.Errorf("dead PID should report an empty state, got %q", info.state)
	}
}

func TestGetProcessTreeMemoryMB_LiveProcess(t *testing.T) {
	mb := getProcessTreeMemoryMB(os.Getpid())
	if mb <= 0 {
		t.Errorf("getProcessTreeMemoryMB(self) = %d, want > 0", mb)
	}
}

func TestGetProcessTreeMemoryMB_DeadProcess(t *testing.T) {
	if mb := getProcessTreeMemoryMB(999999); mb != 0 {
		t.Errorf("getProcessTreeMemoryMB(dead) = %d, want 0", mb)
	}
}

// readProcMemory reads /proc/<pid>/statm, which only exists on Linux. Calling it
// keeps the fallback path exercised on every platform.
func TestReadProcMemory_DoesNotPanic(t *testing.T) {
	if got := readProcMemory(os.Getpid()); got < 0 {
		t.Errorf("readProcMemory = %d, want >= 0", got)
	}
}

func TestFindMainProcessByName_FindsMatch(t *testing.T) {
	cmd := exec.Command("sleep", "45")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start helper process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	if got := findMainProcessByName("sleep", nil); got == 0 {
		t.Error("expected to find a running 'sleep' process")
	}
}

func TestFindMainProcessByName_NoMatch(t *testing.T) {
	if got := findMainProcessByName("definitely-not-a-real-binary-xyz", nil); got != 0 {
		t.Errorf("findMainProcessByName = %d, want 0 for an unknown binary", got)
	}
}

// ── daemonize detection ─────────────────────────────────────

func TestDetectDaemonize_NoPidFileStaysRunning(t *testing.T) {
	name := "no-daemon"
	svc := config.ServiceConfig{WORK_DIR: t.TempDir(), EXEC_CMD: "sleep 20"}

	proc, err := StartProcess(context.Background(), svc, name)
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	defer func() {
		_ = StopProcess(proc, 2*time.Second)
		RemoveServicePidFile(name)
	}()

	sup := NewSupervisor(&config.Config{Services: map[string]config.ServiceConfig{}})
	if got := sup.detectDaemonize(proc, svc, nil); got != 0 {
		t.Errorf("detectDaemonize = %d, want 0 for a process that does not daemonize", got)
	}
}

func TestDetectDaemonize_PidFileUnreadable(t *testing.T) {
	sup := NewSupervisor(&config.Config{Services: map[string]config.ServiceConfig{}})
	proc := &ProcessInfo{Name: "x", Pid: os.Getpid(), StartTime: time.Now()}
	svc := config.ServiceConfig{PID_FILE: filepath.Join("/definitely/not/here", "x.pid")}

	if got := sup.detectDaemonize(proc, svc, nil); got != 0 {
		t.Errorf("detectDaemonize = %d, want 0 when the PID file cannot be read", got)
	}
}

func TestDetectDaemonize_NoCmd(t *testing.T) {
	sup := NewSupervisor(&config.Config{Services: map[string]config.ServiceConfig{}})
	// No Cmd and no PID_FILE → nothing to detect.
	if got := sup.detectDaemonize(&ProcessInfo{Name: "x", Pid: 1}, config.ServiceConfig{}, nil); got != 0 {
		t.Errorf("detectDaemonize = %d, want 0", got)
	}
}

func TestFindDaemonizedMaster_NoMatch(t *testing.T) {
	proc := &ProcessInfo{Name: "x", Pid: os.Getpid()}
	svc := config.ServiceConfig{EXEC_CMD: "definitely-unique-binary-xyz"}

	if got := findDaemonizedMaster(proc, svc, nil); got != 0 {
		t.Errorf("findDaemonizedMaster = %d, want 0 when no replacement exists", got)
	}
}

// ── locking ─────────────────────────────────────────────────

func TestAcquireLock_Exclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gorch.lock")

	if err := AcquireLock(path); err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	// A second, independent open of the same file must not acquire the lock
	// while the first one holds it.
	if err := AcquireLock(path); err == nil {
		t.Error("expected the second AcquireLock to fail while the lock is held")
	}

	ReleaseLock(path)

	if err := AcquireLock(path); err != nil {
		t.Errorf("AcquireLock after release: %v", err)
	}
	ReleaseLock(path)
}

func TestAcquireLock_BadPath(t *testing.T) {
	if err := AcquireLock(filepath.Join("/definitely/not/here", "gorch.lock")); err == nil {
		t.Error("expected an error for an unwritable lock path")
	}
}

func TestReleaseLock_WhenNotHeld(t *testing.T) {
	// Must not panic when no lock was ever acquired.
	ReleaseLock(filepath.Join(t.TempDir(), "unused.lock"))
}

// ── StopService / GetAllStatus ──────────────────────────────

func TestSupervisor_StopService_NotRunning(t *testing.T) {
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{"a": {EXEC_CMD: "true"}},
	})
	if err := sup.StopService(context.Background(), "a"); err == nil {
		t.Error("expected an error when stopping a service that is not running")
	}
}

func TestSupervisor_StopService_Running(t *testing.T) {
	name := "stop-svc"
	svc := config.ServiceConfig{WORK_DIR: t.TempDir(), EXEC_CMD: "sleep 60"}
	sup := NewSupervisor(&config.Config{Services: map[string]config.ServiceConfig{name: svc}})

	proc, err := StartProcess(context.Background(), svc, name)
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	exitCh, err := MonitorProcess(proc)
	if err != nil {
		t.Fatalf("MonitorProcess: %v", err)
	}
	sup.processes[name] = proc

	if err := sup.StopService(context.Background(), name); err != nil {
		t.Fatalf("StopService: %v", err)
	}

	select {
	case <-exitCh:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after StopService")
	}

	if _, tracked := sup.processes[name]; tracked {
		t.Error("service should no longer be tracked after StopService")
	}
}

func TestSupervisor_GetAllStatus_MarksDeadProcessStopped(t *testing.T) {
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{"dead": {EXEC_CMD: "x"}},
	})
	// A tracked process whose PID no longer exists must be reported stopped.
	sup.processes["dead"] = &ProcessInfo{Name: "dead", Pid: 999999, StartTime: time.Now()}

	all := sup.GetAllStatus()
	st, ok := all["dead"]
	if !ok {
		t.Fatal("expected a status entry for 'dead'")
	}
	if st.Status != config.StatusStopped {
		t.Errorf("Status = %q, want stopped", st.Status)
	}
}

func TestSupervisor_GetAllStatus_ReturnsCached(t *testing.T) {
	sup := NewSupervisor(&config.Config{Services: map[string]config.ServiceConfig{}})
	sup.statusCache.Update("cached", status.ServiceStatus{
		Name:   "cached",
		Status: config.StatusRunning,
		Pid:    4242,
	})

	all := sup.GetAllStatus()
	if len(all) != 1 {
		t.Fatalf("GetAllStatus returned %d entries, want 1", len(all))
	}
	if all["cached"].Pid != 4242 {
		t.Errorf("Pid = %d, want 4242", all["cached"].Pid)
	}
}
