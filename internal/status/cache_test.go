package status

import (
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
