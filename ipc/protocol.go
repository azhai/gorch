package ipc

import (
	"encoding/json"
)

type ControlCommand struct {
	Action  string  `json:"action"`
	Service *string `json:"service,omitempty"`
	Config  *string `json:"config,omitempty"`
	Lines   int     `json:"lines,omitempty"`
	Follow  bool    `json:"follow,omitempty"`
	Live    bool    `json:"live,omitempty"`
}

type ControlResponse struct {
	Status  string          `json:"status"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func OkResponse(data any) ControlResponse {
	raw, _ := json.Marshal(data)
	return ControlResponse{Status: "ok", Data: raw}
}

func ErrorResponse(msg string) ControlResponse {
	return ControlResponse{Status: "error", Message: msg}
}
