package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/azhai/gorch/config"
	"github.com/robfig/cron/v3"
)

// nextRunFor computes the next occurrence of a cron expression, used to make
// precise assertions about a job's next run time.
func nextRunFor(t *testing.T, expression string) time.Time {
	t.Helper()
	parser := cron.NewParser(
		cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	sched, err := parser.Parse(expression)
	if err != nil {
		t.Fatalf("bad test expression %q: %v", expression, err)
	}
	return sched.Next(time.Now())
}

// waitUntil polls cond until it returns true or the deadline expires.
func waitUntil(cond func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func hasCronProc(sup *Supervisor, name string) bool {
	sup.mu.RLock()
	defer sup.mu.RUnlock()
	_, ok := sup.cronProcs[name]
	return ok
}

func historyHasStatus(sup *Supervisor, name, status string) bool {
	for _, r := range sup.GetCronScheduler().GetHistory(name) {
		if r.Status == status {
			return true
		}
	}
	return false
}

// ── processAlive ────────────────────────────────────────────

func TestProcessAlive_Nil(t *testing.T) {
	if processAlive(nil) {
		t.Error("nil process must not report alive")
	}
}

func TestProcessAlive_NoCmd(t *testing.T) {
	if processAlive(&ProcessInfo{Name: "ghost"}) {
		t.Error("process without Cmd must not report alive")
	}
}

func TestProcessAlive_RunningThenExited(t *testing.T) {
	dir := t.TempDir()
	svc := config.ServiceConfig{WORK_DIR: dir, EXEC_CMD: "sleep 30"}
	name := "alive-probe"
	defer RemoveServicePidFile(name)

	proc, err := StartProcess(context.Background(), svc, name)
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	if !processAlive(proc) {
		t.Error("just-started process should be alive")
	}

	_ = proc.Cmd.Process.Kill()
	_, _ = proc.Cmd.Process.Wait()

	if processAlive(proc) {
		t.Error("reaped process must not report alive")
	}
}

// ── one-shot cron execution ─────────────────────────────────

// TestMakeCronFn_OneShotCleansUp is the core regression test for the original
// "task never runs again" bug: a cron task is one-shot, so once the process
// exits its slot is released and the next trigger may run.
func TestMakeCronFn_OneShotCleansUp(t *testing.T) {
	dir := t.TempDir()
	name := "oneshot-job"
	defer RemoveServicePidFile(name)

	svc := config.ServiceConfig{WORK_DIR: dir, EXEC_CMD: "true"}
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{name: svc},
	})

	fn := sup.makeCronFn(name, svc)
	fn()

	if !hasCronProc(sup, name) {
		t.Fatal("cron process was not tracked in cronProcs")
	}

	if ok := waitUntil(func() bool { return !hasCronProc(sup, name) }, 10*time.Second); !ok {
		t.Fatal("cron process slot was never released — subsequent triggers would be skipped as overlap")
	}

	if !historyHasStatus(sup, name, "started") {
		t.Errorf("expected a 'started' execution record, got %+v", sup.GetCronScheduler().GetHistory(name))
	}
}

// TestMakeCronFn_SecondRunAfterFirstCompletes proves the slot is reusable:
// after the first run finishes, a second trigger actually starts a process.
func TestMakeCronFn_SecondRunAfterFirstCompletes(t *testing.T) {
	dir := t.TempDir()
	name := "rerun-job"
	defer RemoveServicePidFile(name)

	svc := config.ServiceConfig{WORK_DIR: dir, EXEC_CMD: "true"}
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{name: svc},
	})

	fn := sup.makeCronFn(name, svc)
	fn()
	if ok := waitUntil(func() bool { return !hasCronProc(sup, name) }, 10*time.Second); !ok {
		t.Fatal("first run never finished")
	}

	fn() // second trigger must NOT be skipped as an overlap
	if !hasCronProc(sup, name) {
		t.Fatal("second trigger was skipped — cron task would never run again")
	}
	if ok := waitUntil(func() bool { return !hasCronProc(sup, name) }, 10*time.Second); !ok {
		t.Fatal("second run never finished")
	}

	if historyHasStatus(sup, name, "overlap") {
		t.Error("second run was recorded as an overlap")
	}
}

// TestMakeCronFn_OverlapSkipped verifies a still-running task is not started twice.
func TestMakeCronFn_OverlapSkipped(t *testing.T) {
	dir := t.TempDir()
	name := "overlap-job"
	defer RemoveServicePidFile(name)

	svc := config.ServiceConfig{WORK_DIR: dir, EXEC_CMD: "sleep 30"}
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{name: svc},
	})

	fn := sup.makeCronFn(name, svc)
	fn()
	fn() // second call must be skipped

	if !historyHasStatus(sup, name, "overlap") {
		t.Error("expected an 'overlap' record for the skipped trigger")
	}

	// Clean up the long-running process.
	sup.mu.RLock()
	proc := sup.cronProcs[name]
	sup.mu.RUnlock()
	if proc != nil {
		_ = StopProcess(proc, 2*time.Second)
	}
}

// TestMakeCronFn_StartFailure verifies a failed start is recorded.
func TestMakeCronFn_StartFailure(t *testing.T) {
	name := "fail-job"
	// Non-existent WORK_DIR makes StartProcess fail.
	svc := config.ServiceConfig{WORK_DIR: "/definitely/not/here", EXEC_CMD: "true"}
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{name: svc},
	})

	fn := sup.makeCronFn(name, svc)
	fn()

	if !historyHasStatus(sup, name, "failed") {
		t.Errorf("expected a 'failed' record, got %+v", sup.GetCronScheduler().GetHistory(name))
	}
	if hasCronProc(sup, name) {
		t.Error("failed start must not leave a tracked process")
	}
}

// TestWaitCronProc_TimeoutKillsTask verifies CRON_TIMEOUT stops a task that
// overruns, so it cannot pin the slot forever.
func TestWaitCronProc_TimeoutKillsTask(t *testing.T) {
	dir := t.TempDir()
	name := "timeout-job"
	defer RemoveServicePidFile(name)

	svc := config.ServiceConfig{WORK_DIR: dir, EXEC_CMD: "sleep 60", CRON_TIMEOUT: 1}
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{name: svc},
	})

	sup.makeCronFn(name, svc)()
	if !hasCronProc(sup, name) {
		t.Fatal("task did not start")
	}

	if ok := waitUntil(func() bool { return !hasCronProc(sup, name) }, 15*time.Second); !ok {
		t.Fatal("CRON_TIMEOUT did not release the slot")
	}
}

// ── schedule changes take effect immediately ────────────────

// TestUpdateServiceConfig_CronChangeTakesEffect verifies the user-visible
// requirement: editing a task's schedule applies right away, without a restart.
func TestUpdateServiceConfig_CronChangeTakesEffect(t *testing.T) {
	name := "resched-job"
	oldExpr := "0 0 3 * * *"  // 03:00 daily
	newExpr := "0 0 15 * * *" // 15:00 daily

	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{
			name: {EXEC_CMD: "true", CRON: oldExpr},
		},
	})
	sup.cronSched.Start()
	defer sup.cronSched.Stop()

	if err := sup.rebuildCronScheduler(); err != nil {
		t.Fatalf("rebuildCronScheduler: %v", err)
	}
	before := sup.cronSched.JobNext(name)
	if before.IsZero() {
		t.Fatal("job was not registered")
	}

	updated := config.ServiceConfig{EXEC_CMD: "true", CRON: newExpr}
	if err := sup.UpdateServiceConfig(name, updated); err != nil {
		t.Fatalf("UpdateServiceConfig: %v", err)
	}

	after := sup.cronSched.JobNext(name)
	want := nextRunFor(t, newExpr)
	if !after.Equal(want) {
		t.Errorf("after schedule change next run = %v, want %v (was %v)", after, want, before)
	}
	if after.Equal(before) {
		t.Error("schedule change did not take effect — still using the old plan")
	}
}

// TestUpdateServiceConfig_UnrelatedChangeKeepsNextRun guards the starvation bug:
// rebuilding for an unrelated update must not postpone a job's next run.
func TestUpdateServiceConfig_UnrelatedChangeKeepsNextRun(t *testing.T) {
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{
			"cron-job": {EXEC_CMD: "true", CRON: "0 0 3 * * *"},
			"plain":    {EXEC_CMD: "true"},
		},
	})
	sup.cronSched.Start()
	defer sup.cronSched.Stop()

	if err := sup.rebuildCronScheduler(); err != nil {
		t.Fatal(err)
	}
	before := sup.cronSched.JobNext("cron-job")

	// Edit an unrelated, non-cron service.
	if err := sup.UpdateServiceConfig("plain", config.ServiceConfig{EXEC_CMD: "echo hi"}); err != nil {
		t.Fatal(err)
	}

	if after := sup.cronSched.JobNext("cron-job"); !after.Equal(before) {
		t.Errorf("unrelated update moved next run: %v -> %v", before, after)
	}
}

// TestUpdateServiceConfig_InvalidCronKeepsValidJobs verifies one bad expression
// cannot leave the other schedules unregistered.
func TestUpdateServiceConfig_InvalidCronKeepsValidJobs(t *testing.T) {
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{
			"good": {EXEC_CMD: "true", CRON: "0 0 3 * * *"},
			"bad":  {EXEC_CMD: "true", CRON: "0 0 3 * * *"},
		},
	})

	if err := sup.UpdateServiceConfig("bad", config.ServiceConfig{EXEC_CMD: "true", CRON: "not a cron"}); err == nil {
		t.Fatal("expected an error for an invalid cron expression")
	}
	if !sup.cronSched.HasJob("good") {
		t.Error("valid cron job was dropped because another service had a bad expression")
	}
}

// ── register / rebuild / remove ─────────────────────────────

func TestRebuildCronScheduler_RegistersOnlyCronServices(t *testing.T) {
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{
			"with-cron":    {EXEC_CMD: "true", CRON: "0 0 * * * *"},
			"without-cron": {EXEC_CMD: "true"},
		},
	})

	if err := sup.rebuildCronScheduler(); err != nil {
		t.Fatalf("rebuildCronScheduler: %v", err)
	}

	if !sup.cronSched.HasJob("with-cron") {
		t.Error("service with CRON was not registered")
	}
	if sup.cronSched.HasJob("without-cron") {
		t.Error("service without CRON must not be registered as a cron job")
	}
}

func TestRebuildCronScheduler_RemovesStaleJobs(t *testing.T) {
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{
			"a": {EXEC_CMD: "true", CRON: "0 0 * * * *"},
			"b": {EXEC_CMD: "true", CRON: "0 0 * * * *"},
		},
	})
	if err := sup.rebuildCronScheduler(); err != nil {
		t.Fatal(err)
	}
	if !sup.cronSched.HasJob("b") {
		t.Fatal("b should be registered")
	}

	// Drop "b" from the config and rebuild.
	delete(sup.cfg.Services, "b")
	if err := sup.rebuildCronScheduler(); err != nil {
		t.Fatal(err)
	}
	if sup.cronSched.HasJob("b") {
		t.Error("stale cron job was not removed")
	}
	if !sup.cronSched.HasJob("a") {
		t.Error("job 'a' should still be registered")
	}
}

func TestCreateService_RegistersCronJob(t *testing.T) {
	sup := NewSupervisor(&config.Config{Services: map[string]config.ServiceConfig{}})

	if err := sup.CreateService("new-cron", config.ServiceConfig{EXEC_CMD: "true", CRON: "0 0 * * * *"}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if !sup.cronSched.HasJob("new-cron") {
		t.Error("newly created cron service was not registered")
	}
}

func TestCreateService_Duplicate(t *testing.T) {
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{"dup": {EXEC_CMD: "true"}},
	})
	if err := sup.CreateService("dup", config.ServiceConfig{EXEC_CMD: "true"}); err == nil {
		t.Error("expected error creating a duplicate service")
	}
}

func TestDeleteService_RemovesCronJob(t *testing.T) {
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{
			"doomed": {EXEC_CMD: "true", CRON: "0 0 * * * *"},
		},
	})
	if err := sup.rebuildCronScheduler(); err != nil {
		t.Fatal(err)
	}
	if !sup.cronSched.HasJob("doomed") {
		t.Fatal("job should be registered before deletion")
	}

	if err := sup.DeleteService("doomed"); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}
	if sup.cronSched.HasJob("doomed") {
		t.Error("cron job survived service deletion and would keep firing")
	}
}

func TestDeleteService_NotFound(t *testing.T) {
	sup := NewSupervisor(&config.Config{Services: map[string]config.ServiceConfig{}})
	if err := sup.DeleteService("nope"); err == nil {
		t.Error("expected error deleting an unknown service")
	}
}

func TestStartService_RejectsCronService(t *testing.T) {
	sup := NewSupervisor(&config.Config{
		Services: map[string]config.ServiceConfig{
			"scheduled": {EXEC_CMD: "true", CRON: "0 0 * * * *"},
		},
	})
	err := sup.StartService(context.Background(), "scheduled")
	if err == nil {
		t.Fatal("expected an error when directly starting a cron service")
	}
}

// ── reload from disk ────────────────────────────────────────

// TestHandleReload_PicksUpNewSchedule verifies SIGHUP/reload applies a schedule
// edited directly in the config file.
func TestHandleReload_PicksUpNewSchedule(t *testing.T) {
	file := filepath.Join(t.TempDir(), "gorch.toml")
	writeCfg := func(expr string) {
		body := "[services]\n[services.job]\nCRON = '" + expr + "'\nEXEC_CMD = 'true'\nWORK_DIR = '" + t.TempDir() + "'\n"
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}

	writeCfg("0 0 3 * * *")
	sup := NewSupervisor(&config.Config{Services: map[string]config.ServiceConfig{}}, WithConfigPath(file))
	sup.cronSched.Start()
	defer sup.cronSched.Stop()

	if err := sup.HandleReload(); err != nil {
		t.Fatalf("HandleReload: %v", err)
	}
	if !sup.cronSched.HasJob("job") {
		t.Fatal("job from config file was not registered")
	}

	// Edit the schedule on disk and reload.
	writeCfg("0 0 15 * * *")
	if err := sup.HandleReload(); err != nil {
		t.Fatalf("HandleReload: %v", err)
	}

	want := nextRunFor(t, "0 0 15 * * *")
	if got := sup.cronSched.JobNext("job"); !got.Equal(want) {
		t.Errorf("after reload next run = %v, want %v", got, want)
	}
}

func TestHandleReload_MissingConfigFile(t *testing.T) {
	sup := NewSupervisor(
		&config.Config{Services: map[string]config.ServiceConfig{}},
		WithConfigPath("/definitely/not/here.toml"),
	)
	if err := sup.HandleReload(); err == nil {
		t.Error("expected an error reloading a missing config file")
	}
}
