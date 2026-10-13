package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ToolCallSink is the subset of UISink the tool-call streaming path needs.
type ToolCallSink interface {
	ToolCallStart(agentID, id, toolName, input string)
}
