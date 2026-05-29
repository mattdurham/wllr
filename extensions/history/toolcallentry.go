package main

import "encoding/json"

//lint:ignore U1000 used in WASM build (wasip1 tag)
type toolCallEntry struct {
	Type       string          `json:"type"`
	ID         string          `json:"id"`
	Timestamp  string          `json:"timestamp"`
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input,omitempty"`
}
