package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// SpawnRequest carries the parameters for spawning a sub-agent.
// Mirrors extension.SpawnRequest; defined here to avoid importing extension from agent.
type SpawnRequest struct {
	ID             string
	Name           string
	SystemPrompt   string
	ModelName      string
	InitialPrompt  string
	ThinkingBudget int
}
