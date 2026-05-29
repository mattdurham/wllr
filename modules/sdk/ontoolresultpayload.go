package sdk

// OnToolResultPayload is the payload for EventOnToolResult.
type OnToolResultPayload struct {
	ToolCallID string `json:"tool_call_id"`
	Result     string `json:"result"`
	IsError    bool   `json:"is_error"`
}
