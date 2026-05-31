package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"time"

	"charm.land/fantasy"
)

// SpawnOpts configures a new agent at spawn time.
type SpawnOpts struct {
	SystemPrompt      string
	Name              string
	InheritBasePrompt *bool
	Tools             []fantasy.AgentTool
	ModelName         string
	TurnTimeout       time.Duration
	ThinkingBudget    int
	ProviderOptions   fantasy.ProviderOptions
}
