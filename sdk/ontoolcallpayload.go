package sdk

import "encoding/json"

// OnToolCallPayload is the payload for EventOnToolCall.
type OnToolCallPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
}
