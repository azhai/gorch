//go:build noweb

// This file provides no-op stand-ins for the web layer so that gorch can be
// built without Echo and without the embedded SPA:
//
//	go build -tags noweb ./cmd/gorch
//
// The rest of the package (server.go, handler.go, auth.go, sse.go, static.go)
// is guarded by `//go:build !noweb`, so none of the HTTP, SSE or embedded
// frontend code is compiled — and therefore not linked — into that binary.
package server

import (
	"log/slog"

	"github.com/azhai/gorch/status"
)

// Hub is an empty stand-in for the SSE hub. With no HTTP server there are no
// clients to broadcast to, so every method does nothing.
type Hub struct{}

// NewHub returns a no-op hub.
func NewHub() *Hub { return &Hub{} }

// Run returns immediately; there are no clients to serve.
func (h *Hub) Run() {}

// Stop is a no-op.
func (h *Hub) Stop() {}

// BroadcastStatusChange is a no-op.
func (h *Hub) BroadcastStatusChange(name string, status string, pid int, startedAt int64, rssMB int64) {
}

// BroadcastUptimeTick is a no-op.
func (h *Hub) BroadcastUptimeTick(allStatus map[string]status.ServiceStatus) {}

// Server is an empty stand-in for the embedded web server.
type Server struct{}

// NewServer reports that the web UI is unavailable in this build.
func NewServer(addr string, sup SupervisorProvider) *Server {
	slog.Warn("WEB_ENABLE is set but this binary was built without web support (-tags noweb)", "addr", addr)
	return &Server{}
}

// Start does nothing and never fails.
func (s *Server) Start() error { return nil }

// Stop is a no-op.
func (s *Server) Stop() {}
