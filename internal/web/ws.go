package web

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
)

type WsMessage struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"timestamp"`
}

type StatusChangePayload struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	Pid          int    `json:"pid,omitempty"`
	Uptime       int64  `json:"uptime,omitempty"`
	RestartCount int    `json:"restartCount"`
	ExitCode     *int   `json:"exitCode,omitempty"`
}

type LogLinePayload struct {
	Service string `json:"service"`
	Line    string `json:"line"`
	Stream  string `json:"stream"`
}

type CronResultPayload struct {
	Service   string `json:"service"`
	StartedAt int64  `json:"startedAt"`
	EndedAt   int64  `json:"endedAt,omitempty"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	Status    string `json:"status"`
}

type ConnectionStatusPayload struct {
	Connected bool `json:"connected"`
}

type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan WsMessage
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
	stopCh     chan struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan WsMessage, 256),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		stopCh:     make(chan struct{}),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case <-h.stopCh:
			h.mu.Lock()
			for conn := range h.clients {
				conn.Close()
			}
			h.mu.Unlock()
			return

		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()
			slog.Debug("websocket client connected", "total", len(h.clients))

		case conn := <-h.unregister:
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
			conn.Close()
			slog.Debug("websocket client disconnected", "total", len(h.clients))

		case msg := <-h.broadcast:
			data, _ := json.Marshal(msg)
			h.mu.RLock()
			for conn := range h.clients {
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					h.mu.RUnlock()
					h.mu.Lock()
					delete(h.clients, conn)
					conn.Close()
					h.mu.Unlock()
					h.mu.RLock()
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Stop() {
	close(h.stopCh)
}

func (h *Hub) BroadcastStatusChange(name string, status string, pid int, restartCnt int) {
	payload, _ := json.Marshal(StatusChangePayload{
		Name:         name,
		Status:       status,
		Pid:          pid,
		RestartCount: restartCnt,
	})

	h.broadcast <- WsMessage{
		Type:      "status_change",
		Payload:   payload,
		Timestamp: currentTimeMillis(),
	}
}

func (h *Hub) BroadcastLogLine(service string, line string, stream string) {
	payload, _ := json.Marshal(LogLinePayload{
		Service: service,
		Line:    line,
		Stream:  stream,
	})

	h.broadcast <- WsMessage{
		Type:      "log_line",
		Payload:   payload,
		Timestamp: currentTimeMillis(),
	}
}

func (h *Hub) BroadcastCronResult(service string, startedAt, endedAt int64, exitCode *int, status string) {
	payload, _ := json.Marshal(CronResultPayload{
		Service:   service,
		StartedAt: startedAt,
		EndedAt:   endedAt,
		ExitCode:  exitCode,
		Status:    status,
	})

	h.broadcast <- WsMessage{
		Type:      "cron_result",
		Payload:   payload,
		Timestamp: currentTimeMillis(),
	}
}

func (s *Server) handleWebSocket(c fiber.Ctx) error {
	if !websocket.IsWebSocketUpgrade(c) {
		return fiber.ErrUpgradeRequired
	}

	return websocket.New(func(conn *websocket.Conn) {
		hub := s.supervisor.GetHub()
		hub.register <- conn

		conn.WriteJSON(WsMessage{
			Type:      "connection_status",
			Payload:   mustMarshal(ConnectionStatusPayload{Connected: true}),
			Timestamp: currentTimeMillis(),
		})

		defer func() {
			hub.unregister <- conn
		}()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	})(c)
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

var currentTimeMillis = func() int64 {
	return time.Now().UnixMilli()
}
