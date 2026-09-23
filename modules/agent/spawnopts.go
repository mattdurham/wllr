package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"time"

	"charm.land/fantasy"
)

// SpawnOpts configures a new agent at spawn time.
type SpawnOpts struct {
	InheritBasePrompt *bool
	ProviderOptions   fantasy.ProviderOptions
	SystemPrompt      string
	Name              string
	ModelName         string
	Tools             []fantasy.AgentTool
	ContextWindow     int64
	TurnTimeout       time.Duration
	ThinkingBudget    int
}
