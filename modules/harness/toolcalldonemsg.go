package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ToolCallDoneMsg is sent when a tool call completes (via OnAfterToolCall).
type ToolCallDoneMsg struct {
	ID      string
	Output  string
	IsError bool
}
