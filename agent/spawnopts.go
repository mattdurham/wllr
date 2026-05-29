package agent

import "charm.land/fantasy"

// SpawnOpts configures a new agent at spawn time.
type SpawnOpts struct {
	InheritBasePrompt *bool
	ProviderOptions   fantasy.ProviderOptions
	SystemPrompt      string
	Name              string
	ModelName         string
	// NotifyParentID, if non-empty, causes the pool to automatically send a
	// completion message to that agent when this agent's final turn ends.
	// This gives the parent a guaranteed wakeup without requiring the sub-agent
	// to call send_message explicitly.
	NotifyParentID string
	Tools          []fantasy.AgentTool
	ThinkingBudget int
}
