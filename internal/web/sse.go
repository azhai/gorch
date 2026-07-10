package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/azhai/gorch/internal/status"
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
	clients    map[*sseClient]bool
	broadcast  chan SSEMessage
	register   chan *sseClient
	unregister chan *sseClient
	mu         sync.RWMutex
	stopCh     chan struct{}
}

type sseClient struct {
	ch chan SSEMessage
}

func newSSEClient() *sseClient {
	return &sseClient{ch: make(chan SSEMessage, 64)}
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*sseClient]bool),
		broadcast:  make(chan SSEMessage, 256),
		register:   make(chan *sseClient),
		unregister: make(chan *sseClient),
		stopCh:     make(chan struct{}),
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
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			slog.Debug("sse client connected", "total", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.ch)
			}
			h.mu.Unlock()
			slog.Debug("sse client disconnected", "total", len(h.clients))

		case msg := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.ch <- msg:
				default:
					// client too slow, skip
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Stop() {
	close(h.stopCh)
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
	client := newSSEClient()
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
