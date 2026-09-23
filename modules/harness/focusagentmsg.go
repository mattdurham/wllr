package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// FocusAgentMsg asks the TUI to change which agent receives user input and owns
// the transcript. Emitted by set_focused_agent. An empty AgentID means the root.
type FocusAgentMsg struct {
	AgentID string
}
