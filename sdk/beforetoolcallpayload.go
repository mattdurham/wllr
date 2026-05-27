package sdk

import "encoding/json"

// BeforeToolCallPayload is the payload for EventBeforeToolCall.
type BeforeToolCallPayload struct {
	AgentID    string          `json:"agent_id"`
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
}
