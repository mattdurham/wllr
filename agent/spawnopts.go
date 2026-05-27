package agent

import "charm.land/fantasy"

// SpawnOpts configures a new agent at spawn time.
type SpawnOpts struct {
	InheritBasePrompt *bool
	SystemPrompt      string
	Name              string
	ModelName         string
	Tools             []fantasy.AgentTool
	ThinkingBudget    int
	ProviderOptions   fantasy.ProviderOptions
}
