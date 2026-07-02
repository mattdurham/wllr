package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ToolCallStartMsg is sent when the agent dispatches a tool call.
type ToolCallStartMsg struct {
	AgentID  string
	ID       string
	ToolName string
	Input    string
}
