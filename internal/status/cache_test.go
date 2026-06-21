package status

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/azhai/gorch/internal/config"
)

// ── Test Cache CRUD ──────────────────────────────────────

func TestCache_UpdateAndGet(t *testing.T) {
	c := NewCache()

	st := ServiceStatus{
		Name:   "api",
		Status: config.StatusRunning,
		Pid:    1234,
	}
	c.Update("api", st)

	got, ok := c.Get("api")
	if !ok {
		t.Fatal("expected service 'api' to exist")
	}
	if got.Name != "api" {
		t.Errorf("Name = %q, want 'api'", got.Name)
	}
	if got.Status != config.StatusRunning {
		t.Errorf("Status = %q, want 'running'", got.Status)
	}
	if got.Pid != 1234 {
		t.Errorf("Pid = %d, want 1234", got.Pid)
	}
}

func TestCache_GetMissing(t *testing.T) {
	c := NewCache()

	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected false for missing key")
	}
}

func TestCache_GetAll(t *testing.T) {
	c := NewCache()
	c.Update("a", ServiceStatus{Name: "a", Status: config.StatusRunning})
	c.Update("b", ServiceStatus{Name: "b", Status: config.StatusStopped})

	all := c.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 services, got %d", len(all))
	}
	if all["a"].Status != config.StatusRunning {
		t.Error("service 'a' should be running")
	}
	if all["b"].Status != config.StatusStopped {
		t.Error("service 'b' should be stopped")
	}
}

func TestCache_EmptyNameAutoFill(t *testing.T) {
	c := NewCache()

	c.Update("auto-name", ServiceStatus{Status: config.StatusFailed})

	got, _ := c.Get("auto-name")
	if got.Name != "auto-name" {
		t.Errorf("Name auto-filled = %q, want 'auto-name'", got.Name)
	}
}

func TestCache_Overwrite(t *testing.T) {
	c := NewCache()

	c.Update("svc", ServiceStatus{Name: "svc", Status: config.StatusStarting})
	c.Update("svc", ServiceStatus{Name: "svc", Status: config.StatusRunning, Pid: 5678})

	got, _ := c.Get("svc")
	if got.Status != config.StatusRunning {
		t.Errorf("after overwrite, Status = %q, want 'running'", got.Status)
	}
	if got.Pid != 5678 {
		t.Errorf("after overwrite, Pid = %d, want 5678", got.Pid)
	}
}

// ── Test State Save/Load ─────────────────────────────────

func TestSaveAndLoadState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	services := map[string]ServiceStatus{
		"api": {
			Name:         "api",
			Status:       config.StatusRunning,
			Pid:          1001,
			RestartCount: 2,
		},
		"db": {
			Name:     "db",
			Status:   config.StatusStopped,
			Pid:      0,
			ExitCode: ptr(0),
		},
	}

	err := SaveState(statePath, services)
	if err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Fatal("state file was not created")
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 services in loaded state, got %d", len(loaded))
	}

	api := loaded["api"]
	if api.Status != config.StatusRunning {
		t.Errorf("loaded api.Status = %q, want 'running'", api.Status)
	}
	if api.Pid != 1001 {
		t.Errorf("loaded api.Pid = %d, want 1001", api.Pid)
	}
	if api.RestartCount != 2 {
		t.Errorf("loaded api.RestartCount = %d, want 2", api.RestartCount)
	}

	db := loaded["db"]
	if db.Status != config.StatusStopped {
		t.Errorf("loaded db.Status = %q, want 'stopped'", db.Status)
	}
	if db.ExitCode == nil {
		t.Fatal("expected db.ExitCode to be set")
	}
	if *db.ExitCode != 0 {
		t.Errorf("loaded db.ExitCode = %d, want 0", *db.ExitCode)
	}
}

func TestLoadState_MissingFile(t *testing.T) {
	_, err := LoadState("/nonexistent/state.json")
	if err == nil {
		t.Fatal("expected error for missing state file")
	}
}

func TestSaveLoadState_RoundtripWithExitCode(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "roundtrip.json")

	exitCode := 137
	services := map[string]ServiceStatus{
		"crashed": {
			Name:         "crashed",
			Status:       config.StatusCrashed,
			RestartCount: 3,
			ExitCode:     &exitCode,
		},
	}

	err := SaveState(statePath, services)
	if err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}

	crashed := loaded["crashed"]
	if crashed.Status != config.StatusCrashed {
		t.Errorf("Status = %q, want 'crashed'", crashed.Status)
	}
	if crashed.ExitCode == nil {
		t.Fatal("ExitCode should not be nil")
	}
	if *crashed.ExitCode != 137 {
		t.Errorf("ExitCode = %d, want 137", *crashed.ExitCode)
	}
}

// ── Test JSON serialization ──────────────────────────────

func TestServiceState_JSONRoundTrip(t *testing.T) {
	exitCode := 1
	original := StateFile{
		Services: map[string]ServiceState{
			"test": {
				Status:       config.StatusFailed,
				Pid:          9999,
				RestartCount: 5,
				ExitCode:     &exitCode,
			},
		},
	}

	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var restored StateFile
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	s := restored.Services["test"]
	if s.Status != config.StatusFailed {
		t.Errorf("Status = %q", s.Status)
	}
	if s.Pid != 9999 {
		t.Errorf("Pid = %d", s.Pid)
	}
	if s.RestartCount != 5 {
		t.Errorf("RestartCount = %d", s.RestartCount)
	}
}

// ── Helpers ──────────────────────────────────────────────

func ptr[T any](v T) *T { return &v }
