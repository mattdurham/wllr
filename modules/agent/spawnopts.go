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
	// CreatorID is the ID of the agent that issued the create_agent call that spawned
	// this agent. Empty string for top-level agents (spawned directly by the host or tests).
	// Set by the Spawner when constructing SpawnOpts from an extension.SpawnRequest.
	CreatorID      string
	Tools          []fantasy.AgentTool
	ThinkingBudget int
	// TurnTimeout overrides the per-turn context deadline. Zero uses the default (30m).
	// Set to a negative value to disable the timeout entirely (no deadline).
	TurnTimeout time.Duration
}
