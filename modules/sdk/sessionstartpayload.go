package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// SessionStartPayload is the payload for EventSessionStart.
type SessionStartPayload struct {
	Reason   string          `json:"reason"`
	Tools    []PromptTool    `json:"tools,omitempty"`
	Commands []PromptCommand `json:"commands,omitempty"`
}

// PromptTool is the prompt-relevant subset of a registered tool.
type PromptTool struct {
	Name string `json:"name"`
}

// PromptCommand is the prompt-relevant subset of a registered slash command.
type PromptCommand struct {
	Name string `json:"name"`
	Desc string `json:"description,omitempty"`
}
