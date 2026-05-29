package harness

// ToolCallStartMsg is sent when the agent dispatches a tool call.
type ToolCallStartMsg struct {
	ID       string
	ToolName string
	Input    string
}
