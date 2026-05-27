package harness

// StreamDoneMsg signals that a provider stream has finished.
// AgentID identifies which agent finished; only the main agent's done
// message updates streaming state and finalizes the chat message.
type StreamDoneMsg struct {
	Err     error
	AgentID string
}
