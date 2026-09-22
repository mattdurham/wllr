package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// BeforeAgentStartPayload is the payload for EventBeforeAgentStart.
type BeforeAgentStartPayload struct {
	// AgentID is the agent the turn belongs to, so a transcript can attribute
	// the prompt to the right agent instead of assuming the root.
	AgentID      string `json:"agent_id,omitempty"`
	Prompt       string `json:"prompt"`
	SystemPrompt string `json:"system_prompt"`
	Queued       bool   `json:"queued,omitempty"`
}
