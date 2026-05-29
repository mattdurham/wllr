package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// AgentResetHistoryParams is the params blob for the agent_reset_history host_call.
type AgentResetHistoryParams struct {
	Messages []Message `json:"messages"`
}
