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
	// NotifyParentID, if non-empty, causes the pool to automatically send a
	// completion message to that agent when this agent's final turn ends.
	// This gives the parent a guaranteed wakeup without requiring the sub-agent
	// to call send_message explicitly.
	NotifyParentID string
	Tools          []fantasy.AgentTool
	ThinkingBudget int
	// TurnTimeout overrides the per-turn context deadline. Zero uses the default (30m).
	// Set to a negative value to disable the timeout entirely (no deadline).
	TurnTimeout time.Duration
}
