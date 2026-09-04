package ipc

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recordingHandler records what it was asked to do and replies with a fixed response.
type recordingHandler struct {
	resp  ControlResponse
	last  ControlCommand
	calls int
}

func (h *recordingHandler) HandleCommand(cmd ControlCommand) ControlResponse {
	h.calls++
	h.last = cmd
	return h.resp
}

// startListener runs Listen in the background and waits until the socket exists.
func startListener(t *testing.T, h CommandHandler) (string, *Listener) {
	t.Helper()

	// Use a deliberately short directory: unix socket paths are limited to
	// ~104 bytes on macOS, and t.TempDir() embeds the (long) test name, which
	// made net.Listen fail with "invalid argument" for the longer test names.
	dir, err := os.MkdirTemp("", "gt")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "s.sock")
	l := NewListener(h)
	go l.Listen(path)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("listener never created the socket")
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Cleanup(l.Close)
	return path, l
}

func TestNewListener(t *testing.T) {
	h := &recordingHandler{}
	l := NewListener(h)
	if l == nil {
		t.Fatal("NewListener() returned nil")
	}
	if l.handler == nil {
		t.Error("handler was not stored")
	}
	if l.closed {
		t.Error("new listener should not be closed")
	}
}

// TestListener_RoundTrip verifies a command sent over the socket reaches the
// handler and the reply comes back.
func TestListener_RoundTrip(t *testing.T) {
	h := &recordingHandler{resp: OkResponse(map[string]string{"message": "started"})}
	path, _ := startListener(t, h)

	svc := "api"
	resp, err := SendCommand(path, ControlCommand{Action: "start", Service: &svc})
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok (message=%q)", resp.Status, resp.Message)
	}
	if h.calls != 1 {
		t.Fatalf("handler called %d times, want 1", h.calls)
	}
	if h.last.Action != "start" {
		t.Errorf("handler saw action %q, want start", h.last.Action)
	}
	if h.last.Service == nil || *h.last.Service != "api" {
		t.Errorf("handler saw service %v, want api", h.last.Service)
	}

	var data map[string]string
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data["message"] != "started" {
		t.Errorf("data[message] = %q, want started", data["message"])
	}
}

// TestListener_MultipleCommandsOnOneConnection verifies the loop keeps reading
// commands until the connection is closed.
func TestListener_MultipleCommandsOnOneConnection(t *testing.T) {
	h := &recordingHandler{resp: OkResponse("ok")}
	path, _ := startListener(t, h)

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	for i := 0; i < 3; i++ {
		data, _ := json.Marshal(ControlCommand{Action: "status"})
		if _, err := conn.Write(append(data, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := reader.ReadBytes('\n'); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}

	if h.calls != 3 {
		t.Errorf("handler called %d times, want 3", h.calls)
	}
}

// TestListener_InvalidJSON verifies malformed input yields an error response
// and does not reach the handler.
func TestListener_InvalidJSON(t *testing.T) {
	h := &recordingHandler{resp: OkResponse("ok")}
	path, _ := startListener(t, h)

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("this is not json\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var resp ControlResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Status != "error" {
		t.Errorf("status = %q, want error", resp.Status)
	}
	if !strings.Contains(resp.Message, "invalid command") {
		t.Errorf("message = %q, want it to mention 'invalid command'", resp.Message)
	}
	if h.calls != 0 {
		t.Errorf("handler was called %d times for invalid input, want 0", h.calls)
	}
}

// TestListener_Shutdown verifies the "shutdown" action replies and then ends
// the connection.
func TestListener_Shutdown(t *testing.T) {
	h := &recordingHandler{resp: OkResponse("bye")}
	path, _ := startListener(t, h)

	resp, err := SendCommand(path, ControlCommand{Action: "shutdown"})
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
	if h.last.Action != "shutdown" {
		t.Errorf("handler saw %q, want shutdown", h.last.Action)
	}
}

// TestListener_ListenInvalidPath covers the branch where the socket cannot be
// created: Listen must log and return instead of looping forever.
func TestListener_ListenInvalidPath(t *testing.T) {
	l := NewListener(&recordingHandler{})

	done := make(chan struct{})
	go func() {
		l.Listen(filepath.Join("/definitely/not/here", "s.sock"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Listen did not return when the socket could not be created")
	}
}

func TestListener_CloseIsIdempotent(t *testing.T) {
	h := &recordingHandler{resp: OkResponse("ok")}
	_, l := startListener(t, h)

	l.Close()
	l.Close() // must not panic

	if !l.closed {
		t.Error("listener should be marked closed")
	}
}

func TestSendCommand_NoListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sock")

	_, err := SendCommand(path, ControlCommand{Action: "status"})
	if err == nil {
		t.Fatal("expected an error when nothing is listening")
	}
	if !strings.Contains(err.Error(), "is it running") {
		t.Errorf("error = %q, want it to mention the supervisor is not running", err)
	}
}
