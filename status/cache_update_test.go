package status

import (
	"testing"
	"time"

	"github.com/azhai/gorch/config"
)

func TestCache_Update_FillsName(t *testing.T) {
	c := NewCache()
	c.Update("svc", ServiceStatus{Status: config.StatusRunning})

	st, ok := c.Get("svc")
	if !ok {
		t.Fatal("service not found")
	}
	if st.Name != "svc" {
		t.Errorf("Name = %q, want 'svc'", st.Name)
	}
}

func TestCache_Update_KeepsExplicitName(t *testing.T) {
	c := NewCache()
	c.Update("key", ServiceStatus{Name: "explicit", Status: config.StatusRunning})

	st, _ := c.Get("key")
	if st.Name != "explicit" {
		t.Errorf("Name = %q, want 'explicit'", st.Name)
	}
}

func TestCache_UpdateMemory(t *testing.T) {
	c := NewCache()
	c.Update("svc", ServiceStatus{Name: "svc", Status: config.StatusRunning, MemoryMB: 10})

	c.UpdateMemory("svc", 42)

	st, _ := c.Get("svc")
	if st.MemoryMB != 42 {
		t.Errorf("MemoryMB = %d, want 42", st.MemoryMB)
	}
}

// UpdateMemory on an unknown service must be a no-op rather than creating an entry.
func TestCache_UpdateMemory_UnknownService(t *testing.T) {
	c := NewCache()
	c.UpdateMemory("ghost", 42)

	if _, ok := c.Get("ghost"); ok {
		t.Error("UpdateMemory should not create an entry for an unknown service")
	}
}

// A re-discovered process (new PID) must reset the start time so uptime is accurate.
func TestCache_UpdateProcessInfo_NewPid(t *testing.T) {
	c := NewCache()
	c.Update("svc", ServiceStatus{Name: "svc", Status: config.StatusRunning, Pid: 1, StartedAt: 1000})

	c.UpdateProcessInfo("svc", 2, 30)

	st, _ := c.Get("svc")
	if st.Pid != 2 {
		t.Errorf("Pid = %d, want 2", st.Pid)
	}
	if st.StartedAt <= 1000 {
		t.Errorf("StartedAt = %d, want it refreshed to now when the PID changes", st.StartedAt)
	}
	if st.MemoryMB != 30 {
		t.Errorf("MemoryMB = %d, want 30", st.MemoryMB)
	}
}

// An unchanged PID must NOT reset the start time, otherwise uptime would
// restart from zero on every poll.
func TestCache_UpdateProcessInfo_SamePid(t *testing.T) {
	c := NewCache()
	start := time.Now().Unix()
	c.Update("svc", ServiceStatus{Name: "svc", Status: config.StatusRunning, Pid: 5, StartedAt: start})

	c.UpdateProcessInfo("svc", 5, 77)

	st, _ := c.Get("svc")
	if st.StartedAt != start {
		t.Errorf("StartedAt = %d, want %d (unchanged PID must not reset it)", st.StartedAt, start)
	}
	if st.MemoryMB != 77 {
		t.Errorf("MemoryMB = %d, want 77", st.MemoryMB)
	}
}

func TestCache_UpdateProcessInfo_UnknownService(t *testing.T) {
	c := NewCache()
	c.UpdateProcessInfo("ghost", 9, 9)

	if _, ok := c.Get("ghost"); ok {
		t.Error("UpdateProcessInfo should not create an entry for an unknown service")
	}
}

func TestCache_ComputeUptime_Running(t *testing.T) {
	c := NewCache()
	c.Update("svc", ServiceStatus{
		Name:      "svc",
		Status:    config.StatusRunning,
		StartedAt: time.Now().Add(-3 * time.Second).Unix(),
	})

	st, _ := c.Get("svc")
	if st.Uptime < 2 {
		t.Errorf("Uptime = %d, want >= 2 for a service started 3s ago", st.Uptime)
	}
}

// Uptime is only meaningful while running; a stopped service must report 0.
func TestCache_ComputeUptime_NotRunning(t *testing.T) {
	c := NewCache()
	c.Update("svc", ServiceStatus{
		Name:      "svc",
		Status:    config.StatusStopped,
		StartedAt: time.Now().Add(-time.Hour).Unix(),
	})

	st, _ := c.Get("svc")
	if st.Uptime != 0 {
		t.Errorf("Uptime = %d, want 0 for a stopped service", st.Uptime)
	}
}

func TestCache_ComputeUptime_ZeroStartedAt(t *testing.T) {
	c := NewCache()
	c.Update("svc", ServiceStatus{Name: "svc", Status: config.StatusRunning, StartedAt: 0})

	st, _ := c.Get("svc")
	if st.Uptime != 0 {
		t.Errorf("Uptime = %d, want 0 when StartedAt is unset", st.Uptime)
	}
}

func TestCache_GetAll_ComputesUptime(t *testing.T) {
	c := NewCache()
	c.Update("a", ServiceStatus{Name: "a", Status: config.StatusRunning, StartedAt: time.Now().Add(-2 * time.Second).Unix()})
	c.Update("b", ServiceStatus{Name: "b", Status: config.StatusStopped})

	all := c.GetAll()
	if len(all) != 2 {
		t.Fatalf("GetAll returned %d entries, want 2", len(all))
	}
	if all["a"].Uptime < 1 {
		t.Errorf("a Uptime = %d, want >= 1", all["a"].Uptime)
	}
	if all["b"].Uptime != 0 {
		t.Errorf("b Uptime = %d, want 0 (stopped)", all["b"].Uptime)
	}
}

func TestCache_Get_NotFound(t *testing.T) {
	c := NewCache()
	if _, ok := c.Get("nope"); ok {
		t.Error("Get should report false for an unknown service")
	}
}
