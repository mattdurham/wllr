package agent

// SpawnOpts configures a new agent at spawn time.

// InheritBasePrompt controls whether this agent inherits the pool's
// accumulated base system prompt (AGENTS.md, tool list, action rules).
// Defaults to true. Set false for focused sub-agents that don't need
// the full orchestration context.

// SystemPrompt is passed as the agent's system prompt on each turn.

// Name is the human-readable display name for the agent.

// ModelName overrides the pool's default model name for context-window
// sizing during compaction. If empty, the pool default is used.

// Tools are the tools available to the agent.

// ThinkingBudget enables extended thinking for the agent with the given
// token budget. Only supported on Anthropic models. Zero means disabled.
// The harness wires this into fantasy.WithProviderOptions using the
// provider-specific options struct.

// ProviderOptions are passed directly to fantasy.WithProviderOptions when
// the agent runs each turn. Use this for provider-specific settings such
// as extended thinking (Anthropic) or effort levels.
