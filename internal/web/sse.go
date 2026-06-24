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
	Name     string `json:"name"`
	Status   string `json:"status"`
	Pid      int    `json:"pid,omitempty"`
	Uptime   int64  `json:"uptime,omitempty"`
	MemoryMB int64  `json:"memoryMB"`
	ExitCode *int   `json:"exitCode,omitempty"`
}

type UptimeTickPayload struct {
	Services map[string]UptimeInfo `json:"services"`
}

type UptimeInfo struct {
	Pid      int   `json:"pid"`
	Uptime   int64 `json:"uptime"`
	MemoryMB int64 `json:"memoryMB"`
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

func (h *Hub) BroadcastStatusChange(name string, status string, pid int, uptime int64, rssMB int64) {
	payload, _ := json.Marshal(StatusChangePayload{
		Name:     name,
		Status:   status,
		Pid:      pid,
		Uptime:   uptime,
		MemoryMB: rssMB,
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
				Pid:      st.Pid,
				Uptime:   st.Uptime,
				MemoryMB: st.MemoryMB,
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
	// Set SSE headers
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("X-Accel-Buffering", "no")

	// Enable streaming
	c.Response().WriteHeader(http.StatusOK)
	client := newSSEClient()
	hub := s.supervisor.GetHub()
	hub.register <- client

	defer func() {
		hub.unregister <- client
	}()

	// Send initial connection event
	fmt.Fprintf(c.Response(), "event: connected\ndata: {}\n\n")
	c.Response().Flush()

	ctx := c.Request().Context()

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-client.ch:
			if !ok {
				return nil
			}
			data, _ := json.Marshal(msg)
			fmt.Fprintf(c.Response(), "event: %s\ndata: %s\n\n", msg.Type, data)
			c.Response().Flush()
		}
	}
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
