package cron

import (
	"encoding/json"
	"testing"
	"time"
)

// ── Test Scheduler lifecycle ─────────────────────────────

func TestNewScheduler(t *testing.T) {
	s := NewScheduler()
	if s == nil {
		t.Fatal("NewScheduler() returned nil")
	}
	if s.cron == nil {
		t.Error("cron engine not initialized")
	}
	if s.records == nil {
		t.Error("records map not initialized")
	}
	if s.running == nil {
		t.Error("running map not initialized")
	}
}

func TestScheduler_StartStop(t *testing.T) {
	s := NewScheduler()
	s.Start()
	s.Stop() // should not panic or hang
}

// ── Test AddJob ──────────────────────────────────────────

func TestAddJob_ValidExpression(t *testing.T) {
	s := NewScheduler()

	called := false
	err := s.AddJob("test", "* * * * * *", "", func() { called = true })
	if err != nil {
		t.Fatalf("AddJob() error = %v", err)
	}
	if called {
		t.Error("job should not have executed yet")
	}
}

func TestAddJob_WithSeconds(t *testing.T) {
	s := NewScheduler()

	err := s.AddJob("test-seconds", "*/5 * * * * *", "", func() {})
	if err != nil {
		t.Fatalf("AddJob with seconds error = %v", err)
	}
}

func TestAddJob_InvalidExpression(t *testing.T) {
	s := NewScheduler()

	err := s.AddJob("bad", "not a cron expr", "", func() {})
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestAddJob_InvalidTimezone(t *testing.T) {
	s := NewScheduler()

	err := s.AddJob("tz-test", "* * * * *", "Not/A/Timezone", func() {})
	if err == nil {
		t.Fatal("expected error for invalid timezone")
	}
}

func TestAddJob_ValidTimezone(t *testing.T) {
	s := NewScheduler()

	err := s.AddJob("tz-ok", "* * * * * *", "UTC", func() {})
	if err != nil {
		t.Fatalf("AddJob with UTC timezone error = %v", err)
	}
}

// ── Test Execution Records ───────────────────────────────

func TestRecordExecution(t *testing.T) {
	s := NewScheduler()

	now := time.Now()
	record1 := CronExecutionRecord{
		Service:   "backup",
		StartedAt: now,
		Status:    "success",
		Pid:       1234,
	}
	s.RecordExecution("backup", record1)

	history := s.GetHistory("backup")
	if len(history) != 1 {
		t.Fatalf("expected 1 record, got %d", len(history))
	}
	if history[0].Status != "success" {
		t.Errorf("record status = %q, want 'success'", history[0].Status)
	}
	if history[0].Pid != 1234 {
		t.Errorf("record pid = %d, want 1234", history[0].Pid)
	}
}

func TestRecordExecution_MultipleRecords(t *testing.T) {
	s := NewScheduler()

	for i := 0; i < 5; i++ {
		s.RecordExecution("job", CronExecutionRecord{
			Service: "job",
			Status:  "success",
			Pid:     1000 + i,
		})
	}

	history := s.GetHistory("job")
	if len(history) != 5 {
		t.Errorf("expected 5 records, got %d", len(history))
	}
}

func TestRecordExecution_MaxCapacity(t *testing.T) {
	s := NewScheduler()

	// Add more than max (10) records
	for i := 0; i < 15; i++ {
		s.RecordExecution("capped", CronExecutionRecord{
			Service: "capped",
			Status:  "ok",
			Pid:     i,
		})
	}

	history := s.GetHistory("capped")
	// Should be capped at 10
	if len(history) != 10 {
		t.Errorf("expected max 10 records, got %d", len(history))
	}
	// Should keep the most recent ones (last 10)
	if history[0].Pid != 5 {
		t.Errorf("oldest record should have Pid=5, got %d", history[0].Pid)
	}
	if history[9].Pid != 14 {
		t.Errorf("newest record should have Pid=14, got %d", history[9].Pid)
	}
}

func TestGetHistory_EmptyService(t *testing.T) {
	s := NewScheduler()

	history := s.GetHistory("nonexistent")
	if len(history) != 0 {
		t.Errorf("expected empty history for nonexistent service, got %d", len(history))
	}
}

func TestGetHistory_WithExitCode(t *testing.T) {
	s := NewScheduler()

	exitCode := 1
	now := time.Now()
	record := CronExecutionRecord{
		Service:   "failing-job",
		StartedAt: now,
		EndedAt:   &now,
		ExitCode:  &exitCode,
		Status:    "failed",
		Pid:       9999,
	}
	s.RecordExecution("failing-job", record)

	history := s.GetHistory("failing-job")
	if len(history) != 1 {
		t.Fatal("expected 1 record")
	}
	r := history[0]
	if r.Status != "failed" {
		t.Errorf("status = %q, want 'failed'", r.Status)
	}
	if r.ExitCode == nil {
		t.Fatal("ExitCode should not be nil")
	}
	if *r.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", *r.ExitCode)
	}
	if r.Pid != 9999 {
		t.Errorf("Pid = %d, want 9999", r.Pid)
	}
}

func TestRecordExecution_DifferentServices(t *testing.T) {
	s := NewScheduler()

	s.RecordExecution("svc-a", CronExecutionRecord{Service: "svc-a", Status: "ok"})
	s.RecordExecution("svc-b", CronExecutionRecord{Service: "svc-b", Status: "ok"})
	s.RecordExecution("svc-a", CronExecutionRecord{Service: "svc-a", Status: "ok"})

	if len(s.GetHistory("svc-a")) != 2 {
		t.Error("svc-a should have 2 records")
	}
	if len(s.GetHistory("svc-b")) != 1 {
		t.Error("svc-b should have 1 record")
	}
}

// ── Test Record JSON serialization ───────────────────────

func TestCronExecutionRecord_JSON(t *testing.T) {
	exitCode := 0
	now := time.Now()
	record := CronExecutionRecord{
		Service:   "test-service",
		StartedAt: now,
		EndedAt:   &now,
		ExitCode:  &exitCode,
		Status:    "success",
		Pid:       42,
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var restored CronExecutionRecord
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	if restored.Service != "test-service" {
		t.Errorf("Service = %q", restored.Service)
	}
	if restored.Status != "success" {
		t.Errorf("Status = %q", restored.Status)
	}
	if restored.Pid != 42 {
		t.Errorf("Pid = %d", restored.Pid)
	}
}
