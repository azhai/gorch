package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/azhai/gorch/status"
	"github.com/labstack/echo/v4"
)

type SSEMessage struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"timestamp"`
}

type StatusChangePayload struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Pid       int    `json:"pid,omitempty"`
	MemoryMB  int64  `json:"memoryMB"`
	StartedAt int64  `json:"startedAt,omitempty"`
	ExitCode  *int   `json:"exitCode,omitempty"`
}

type UptimeTickPayload struct {
	Services map[string]UptimeInfo `json:"services"`
}

type UptimeInfo struct {
	Pid       int   `json:"pid"`
	MemoryMB  int64 `json:"memoryMB"`
	StartedAt int64 `json:"startedAt,omitempty"`
}

type Hub struct {
	clients     map[*sseClient]bool
	clientsByID map[string]*sseClient
	broadcast   chan SSEMessage
	register    chan *sseClient
	unregister  chan *sseClient
	mu          sync.RWMutex
	stopCh      chan struct{}
	stopOnce    sync.Once
}

type sseClient struct {
	id string
	ch chan SSEMessage
}

func newSSEClient(id string) *sseClient {
	return &sseClient{id: id, ch: make(chan SSEMessage, 64)}
}

func NewHub() *Hub {
	return &Hub{
		clients:     make(map[*sseClient]bool),
		clientsByID: make(map[string]*sseClient),
		broadcast:   make(chan SSEMessage, 256),
		register:    make(chan *sseClient),
		unregister:  make(chan *sseClient),
		stopCh:      make(chan struct{}),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case <-h.stopCh:
			h.mu.Lock()
			for client := range h.clients {
				close(client.ch)
			}
			h.clientsByID = make(map[string]*sseClient)
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			if existing, ok := h.clientsByID[client.id]; ok {
				select {
				case existing.ch <- SSEMessage{Type: "replaced"}:
				default:
				}
				delete(h.clients, existing)
				close(existing.ch)
			}
			h.clients[client] = true
			h.clientsByID[client.id] = client
			h.mu.Unlock()
			slog.Debug("sse client connected", "id", client.id, "total", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				if existing, ok := h.clientsByID[client.id]; ok && existing == client {
					delete(h.clientsByID, client.id)
				}
				close(client.ch)
			}
			h.mu.Unlock()
			slog.Debug("sse client disconnected", "id", client.id, "total", len(h.clients))

		case msg := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.ch <- msg:
				default:
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Stop() {
	// Guard with sync.Once: Supervisor.Stop may be invoked more than once
	// (e.g. on shutdown while a previous stop is still in flight), and
	// closing an already-closed channel panics.
	h.stopOnce.Do(func() {
		close(h.stopCh)
	})
}

func (h *Hub) BroadcastStatusChange(name string, status string, pid int, startedAt int64, rssMB int64) {
	payload, _ := json.Marshal(StatusChangePayload{
		Name:      name,
		Status:    status,
		Pid:       pid,
		MemoryMB:  rssMB,
		StartedAt: startedAt,
	})

	h.broadcast <- SSEMessage{
		Type:      "status_change",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
}

func (h *Hub) BroadcastUptimeTick(allStatus map[string]status.ServiceStatus) {
	uptimes := make(map[string]UptimeInfo, len(allStatus))
	for name, st := range allStatus {
		if st.Status == "running" {
			uptimes[name] = UptimeInfo{
				Pid:       st.Pid,
				MemoryMB:  st.MemoryMB,
				StartedAt: st.StartedAt,
			}
		}
	}
	if len(uptimes) == 0 {
		return
	}

	payload, _ := json.Marshal(UptimeTickPayload{Services: uptimes})
	h.broadcast <- SSEMessage{
		Type:      "uptime_tick",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
}

func (s *Server) handleSSE(c echo.Context) error {
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("X-Accel-Buffering", "no")

	interval := 5 * time.Second
	if v := c.QueryParam("interval"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 5*time.Second {
			interval = d
		}
	}

	c.Response().WriteHeader(http.StatusOK)
	clientID := c.QueryParam("token")
	if clientID == "" {
		clientID = c.Request().RemoteAddr
	}
	client := newSSEClient(clientID)
	hub := s.supervisor.GetHub()
	hub.register <- client

	defer func() {
		hub.unregister <- client
	}()

	fmt.Fprintf(c.Response(), "event: connected\ndata: {}\n\n")
	c.Response().Flush()

	now := time.Now()
	nextTick := now.Truncate(interval).Add(interval)
	initialSent := false

	ctx := c.Request().Context()

	buildInitialTick := func() SSEMessage {
		allStatus := s.supervisor.GetAllStatus()
		uptimes := make(map[string]UptimeInfo, len(allStatus))
		for name, st := range allStatus {
			if st.Status == "running" {
				uptimes[name] = UptimeInfo{
					Pid:       st.Pid,
					MemoryMB:  st.MemoryMB,
					StartedAt: st.StartedAt,
				}
			}
		}
		payload, _ := json.Marshal(UptimeTickPayload{Services: uptimes})
		return SSEMessage{
			Type:      "uptime_tick",
			Payload:   payload,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	sendMsg := func(msg SSEMessage) {
		data, _ := json.Marshal(msg)
		fmt.Fprintf(c.Response(), "event: %s\ndata: %s\n\n", msg.Type, data)
		c.Response().Flush()
	}

	sendMsg(buildInitialTick())
	initialSent = true

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-client.ch:
			if !ok {
				return nil
			}
			if msg.Type == "replaced" {
				sendMsg(msg)
				return nil
			}
			if msg.Type != "uptime_tick" {
				sendMsg(msg)
				continue
			}
			if !initialSent {
				sendMsg(msg)
				initialSent = true
				nextTick = time.Now().Truncate(interval).Add(interval)
				continue
			}
			now = time.Now()
			if now.Before(nextTick) {
				continue
			}
			sendMsg(msg)
			nextTick = now.Truncate(interval).Add(interval)
		}
	}
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
