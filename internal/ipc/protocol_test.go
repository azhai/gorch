package ipc

import (
	"encoding/json"
	"testing"
)

// ── Test OkResponse / ErrorResponse ──────────────────────

func TestOkResponse(t *testing.T) {
	resp := OkResponse(map[string]string{"message": "started"})

	if resp.Status != "ok" {
		t.Errorf("Status = %q, want 'ok'", resp.Status)
	}
	if resp.Message != "" {
		t.Errorf("Message should be empty, got %q", resp.Message)
	}
	if len(resp.Data) == 0 {
		t.Fatal("Data should not be empty")
	}

	var data map[string]string
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("unmarshal Data error = %v", err)
	}
	if data["message"] != "started" {
		t.Errorf("data[message] = %q, want 'started'", data["message"])
	}
}

func TestErrorResponse(t *testing.T) {
	resp := ErrorResponse("something went wrong")

	if resp.Status != "error" {
		t.Errorf("Status = %q, want 'error'", resp.Status)
	}
	if resp.Message != "something went wrong" {
		t.Errorf("Message = %q, want 'something went wrong'", resp.Message)
	}
	if len(resp.Data) != 0 {
		t.Error("Data should be empty for error response")
	}
}

// ── Test ControlCommand JSON ─────────────────────────────

func TestControlCommand_Marshal(t *testing.T) {
	svcName := "api"
	cmd := ControlCommand{
		Action:  "start",
		Service: &svcName,
		Lines:   100,
		Follow:  true,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var restored ControlCommand
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	if restored.Action != "start" {
		t.Errorf("Action = %q, want 'start'", restored.Action)
	}
	if restored.Service == nil || *restored.Service != "api" {
		t.Error("Service should be 'api'")
	}
	if restored.Lines != 100 {
		t.Errorf("Lines = %d, want 100", restored.Lines)
	}
	if !restored.Follow {
		t.Error("Follow should be true")
	}
}

func TestControlCommand_OptionalFields(t *testing.T) {
	cmd := ControlCommand{Action: "shutdown"}

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var restored ControlCommand
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	if restored.Action != "shutdown" {
		t.Errorf("Action = %q", restored.Action)
	}
	if restored.Service != nil {
		t.Error("Service should be nil for shutdown")
	}
	if restored.Lines != 0 {
		t.Errorf("Lines default should be 0, got %d", restored.Lines)
	}
	if restored.Follow {
		t.Error("Follow default should be false")
	}
}

func TestControlResponse_Marshal(t *testing.T) {
	resp := ControlResponse{
		Status:  "ok",
		Message: "service started",
		Data:    json.RawMessage(`{"pid":1234}`),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var restored ControlResponse
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	if restored.Status != "ok" {
		t.Errorf("Status = %q", restored.Status)
	}
	if restored.Message != "service started" {
		t.Errorf("Message = %q", restored.Message)
	}
}

// ── Test all action types roundtrip ──────────────────────

func TestControlCommand_AllActions(t *testing.T) {
	actions := []string{"status", "start", "stop", "restart", "shutdown", "logs"}
	svc := "test-service"

	for _, action := range actions {
		cmd := ControlCommand{Action: action, Service: &svc}
		data, err := json.Marshal(cmd)
		if err != nil {
			t.Fatalf("Marshal action=%q error = %v", action, err)
		}

		var restored ControlCommand
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("Unmarshal action=%q error = %v", action, err)
		}

		if restored.Action != action {
			t.Errorf("restored Action = %q, want %q", restored.Action, action)
		}
	}
}
