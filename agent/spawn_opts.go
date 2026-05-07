package agent

import "charm.land/fantasy"

// SpawnOpts configures a new agent at spawn time.
type SpawnOpts struct {
	// SystemPrompt is passed as the agent's system prompt on each turn.
	SystemPrompt string
	// Tools are the tools available to the agent.
	Tools []fantasy.AgentTool
}
