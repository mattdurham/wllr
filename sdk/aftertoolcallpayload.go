package sdk

// AfterToolCallPayload is the payload for EventAfterToolCall.
type AfterToolCallPayload struct {
	AgentID    string `json:"agent_id"`
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Result     string `json:"result"`
	IsError    bool   `json:"is_error"`
}
