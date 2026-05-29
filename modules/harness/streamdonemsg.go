package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// StreamDoneMsg signals that a provider stream has finished.
// AgentID identifies which agent finished; only the main agent's done
// message updates streaming state and finalizes the chat message.
type StreamDoneMsg struct {
	Err     error
	AgentID string
}
