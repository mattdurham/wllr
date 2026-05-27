package sdk

// AgentResetHistoryParams is the params blob for the agent_reset_history host_call.
type AgentResetHistoryParams struct {
	Messages []Message `json:"messages"`
}
