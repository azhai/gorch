package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
)

type Listener struct {
	handler  CommandHandler
	listener net.Listener
	mu       sync.Mutex
	closed   bool
}

func NewListener(handler CommandHandler) *Listener {
	return &Listener{handler: handler}
}

func (l *Listener) Listen(socketPath string) {
	os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		slog.Error("failed to listen on unix socket", "path", socketPath, "error", err)
		return
	}
	l.listener = ln
	os.Chmod(socketPath, 0600)

	slog.Info("IPC listening", "socket", socketPath)

	for {
		conn, err := ln.Accept()
		if err != nil {
			l.mu.Lock()
			closed := l.closed
			l.mu.Unlock()
			if closed {
				return
			}
			slog.Error("accept error", "error", err)
			continue
		}

		go l.handleConn(conn)
	}
}

func (l *Listener) handleConn(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()
		var cmd ControlCommand
		if err := json.Unmarshal(line, &cmd); err != nil {
			resp := ErrorResponse(fmt.Sprintf("invalid command: %v", err))
			writeResponse(conn, resp)
			continue
		}

		resp := l.handler.HandleCommand(cmd)
		writeResponse(conn, resp)

		if cmd.Action == "shutdown" {
			return
		}
	}
}

func (l *Listener) Close() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()

	if l.listener != nil {
		l.listener.Close()
	}
}

func writeResponse(conn net.Conn, resp ControlResponse) {
	data, _ := json.Marshal(resp)
	conn.Write(append(data, '\n'))
}

func SendCommand(socketPath string, cmd ControlCommand) (ControlResponse, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return ControlResponse{}, fmt.Errorf("cannot connect to supervisor, is it running?")
	}
	defer conn.Close()

	data, _ := json.Marshal(cmd)
	conn.Write(append(data, '\n'))

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return ControlResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	var resp ControlResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return ControlResponse{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp, nil
}
