package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// AgentUISink is the subset of UISink the main-agent callback wiring needs:
// the streaming effects produced while a turn runs.
type AgentUISink interface {
	Token(text string)
	ToolCallStart(agentID, id, toolName, input string)
	StreamDone(err error)
	AgentWakeup(agentID string)
	Notify(text string)
}
