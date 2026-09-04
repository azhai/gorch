package web

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/azhai/gorch/config"
	"github.com/azhai/gorch/status"
)

// startHub runs the hub and registers the given client, waiting until the
// registration has actually been processed before returning.
func startHub(t *testing.T, h *Hub, id string) *sseClient {
	t.Helper()
	go h.Run()
	t.Cleanup(h.Stop)

	c := newSSEClient(id)
	h.register <- c
	return waitForClient(t, h, id)
}

func waitForClient(t *testing.T, h *Hub, id string) *sseClient {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.mu.RLock()
		c, ok := h.clientsByID[id]
		h.mu.RUnlock()
		if ok {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("client %q was never registered", id)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNewHub(t *testing.T) {
	h := NewHub()
	if h == nil {
		t.Fatal("NewHub() returned nil")
	}
	if h.clients == nil || h.clientsByID == nil {
		t.Error("client maps must be initialized")
	}
	if h.broadcast == nil || h.register == nil || h.unregister == nil || h.stopCh == nil {
		t.Error("hub channels must be initialized")
	}
}

// TestHub_RegisterAndBroadcast verifies a registered client receives broadcasts.
func TestHub_RegisterAndBroadcast(t *testing.T) {
	h := NewHub()
	c := startHub(t, h, "c1")

	h.BroadcastStatusChange("api", "running", 4242, 1700000000, 16)

	select {
	case msg := <-c.ch:
		if msg.Type != "status_change" {
			t.Errorf("type = %q, want status_change", msg.Type)
		}
		var p StatusChangePayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if p.Name != "api" {
			t.Errorf("payload name = %q, want api", p.Name)
		}
		if p.Pid != 4242 {
			t.Errorf("payload pid = %d, want 4242", p.Pid)
		}
		if p.MemoryMB != 16 {
			t.Errorf("payload memoryMB = %d, want 16", p.MemoryMB)
		}
		if p.StartedAt != 1700000000 {
			t.Errorf("payload startedAt = %d, want 1700000000", p.StartedAt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client received no broadcast")
	}
}

// TestHub_RegisterReplacesSameID verifies a reconnect with the same client ID
// replaces (and closes) the previous connection instead of leaking it.
func TestHub_RegisterReplacesSameID(t *testing.T) {
	h := NewHub()
	first := startHub(t, h, "same")

	second := newSSEClient("same")
	h.register <- second

	select {
	case msg, ok := <-first.ch:
		if ok && msg.Type != "replaced" {
			t.Errorf("first client got %+v (open=%v), want a 'replaced' message", msg, ok)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the previous client was neither notified nor closed")
	}

	if got := waitForClient(t, h, "same"); got != second {
		t.Error("the new client should replace the old one under the same ID")
	}
}

func TestHub_Unregister(t *testing.T) {
	h := NewHub()
	c := startHub(t, h, "u1")

	h.unregister <- c

	deadline := time.Now().Add(3 * time.Second)
	for {
		h.mu.RLock()
		_, still := h.clients[c]
		h.mu.RUnlock()
		if !still {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("client was not removed on unregister")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The client's channel must have been closed.
	select {
	case _, ok := <-c.ch:
		if ok {
			t.Error("expected the client channel to be closed after unregister")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client channel was not closed")
	}
}

// TestHub_StopIsIdempotent guards the panic fix: Supervisor.Stop can run more
// than once, and closing an already-closed channel would panic.
func TestHub_StopIsIdempotent(t *testing.T) {
	h := NewHub()

	done := make(chan struct{})
	go func() {
		h.Run()
		close(done)
	}()

	h.Stop()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after Stop")
	}

	h.Stop() // second call must not panic
}

// TestHub_StopClosesClients verifies stopping the hub closes every client.
func TestHub_StopClosesClients(t *testing.T) {
	h := NewHub()
	c := startHub(t, h, "s1")

	h.Stop()

	select {
	case _, ok := <-c.ch:
		if ok {
			t.Error("expected the client channel to be closed when the hub stops")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client channel was not closed on hub stop")
	}
}

// TestHub_UptimeTickOnlyRunning verifies the tick omits non-running services
// (this is what makes the UI show a dash for absent services).
func TestHub_UptimeTickOnlyRunning(t *testing.T) {
	h := NewHub()
	c := startHub(t, h, "t1")

	// Nothing running → the hub should not send a tick at all.
	h.BroadcastUptimeTick(map[string]status.ServiceStatus{
		"stopped": {Name: "stopped", Status: config.StatusStopped},
	})
	select {
	case msg := <-c.ch:
		t.Errorf("unexpected tick for a non-running service: %+v", msg)
	case <-time.After(200 * time.Millisecond):
	}

	// A running service is included.
	h.BroadcastUptimeTick(map[string]status.ServiceStatus{
		"api": {Name: "api", Status: config.StatusRunning, Pid: 77, MemoryMB: 5, StartedAt: 1700000000},
	})
	select {
	case msg := <-c.ch:
		if msg.Type != "uptime_tick" {
			t.Errorf("type = %q, want uptime_tick", msg.Type)
		}
		var p UptimeTickPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		info, ok := p.Services["api"]
		if !ok {
			t.Fatal("running service missing from the tick payload")
		}
		if info.Pid != 77 || info.StartedAt != 1700000000 {
			t.Errorf("tick info = %+v, want pid=77 startedAt=1700000000", info)
		}
		if _, leaked := p.Services["stopped"]; leaked {
			t.Error("non-running services must not appear in the tick")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no uptime tick received")
	}
}

// TestHub_BroadcastToMultipleClients verifies every connected client gets the message.
func TestHub_BroadcastToMultipleClients(t *testing.T) {
	h := NewHub()
	go h.Run()
	t.Cleanup(h.Stop)

	var clients []*sseClient
	for _, id := range []string{"a", "b", "c"} {
		c := newSSEClient(id)
		h.register <- c
		clients = append(clients, waitForClient(t, h, id))
	}

	h.BroadcastStatusChange("api", "stopped", 0, 0, 0)

	for i, c := range clients {
		select {
		case msg := <-c.ch:
			if msg.Type != "status_change" {
				t.Errorf("client %d: type = %q", i, msg.Type)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("client %d received no broadcast", i)
		}
	}
}
