package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// AgentGetHistoryParams is the params blob for the agent_get_history host_call.
type AgentGetHistoryParams struct {
	// ID is the agent whose history to return. Empty means the root agent.
	ID string `json:"id,omitempty"`
}

// AgentGetHistoryResult is the result of the agent_get_history host_call.
type AgentGetHistoryResult struct {
	ID string `json:"id"`
	// Messages is the agent's conversation in order, including the system
	// messages the agent recorded. Views that only want the visible
	// conversation should filter on Message.Type.
	Messages []Message `json:"messages"`
}
