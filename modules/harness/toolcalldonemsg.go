package harness

// ToolCallDoneMsg is sent when a tool call completes (via OnAfterToolCall).
type ToolCallDoneMsg struct {
	ID      string
	Output  string
	IsError bool
}
