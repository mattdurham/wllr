package agent

import "charm.land/fantasy"

// SpawnOpts configures a new agent at spawn time.
type SpawnOpts struct {
	// SystemPrompt is passed as the agent's system prompt on each turn.
	SystemPrompt string
	// Name is the human-readable display name for the agent.
	Name string
	// Tools are the tools available to the agent.
	Tools []fantasy.AgentTool
	// ModelName overrides the pool's default model name for context-window
	// sizing during compaction. If empty, the pool default is used.
	ModelName string
	// InheritBasePrompt controls whether this agent inherits the pool's
	// accumulated base system prompt (AGENTS.md, tool list, action rules).
	// Defaults to true. Set false for focused sub-agents that don't need
	// the full orchestration context.
	InheritBasePrompt *bool
}
