package sdk

// BeforeAgentStartPayload is the payload for EventBeforeAgentStart.
type BeforeAgentStartPayload struct {
	Prompt       string `json:"prompt"`
	SystemPrompt string `json:"system_prompt"`
}
