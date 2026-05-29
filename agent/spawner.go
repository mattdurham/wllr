package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"charm.land/fantasy"
	anthropicprovider "charm.land/fantasy/providers/anthropic"
)

// ToolsFn is a function that returns the current tool list for an agent.
type ToolsFn func(agentID string) []fantasy.AgentTool

// NotifyFn is called when a sub-agent error occurs; it routes to the TUI via program.Send.
type NotifyFn func(text string)

// Spawner creates sub-agents in a pool with appropriate callbacks and conventions.
// It encapsulates the parent-ID derivation, agent-identity system prompt suffix,
// and provider-option construction that was previously inline in harness/model.go SetProgram.
type Spawner struct {
	pool     *AgentPool
	toolsFn  ToolsFn
	notifyFn NotifyFn
}

// NewSpawner creates a Spawner bound to the given pool.
// toolsFn is called on each sub-agent turn to get its tool list.
// notifyFn is called when a sub-agent errors; may be nil.
func NewSpawner(pool *AgentPool, toolsFn ToolsFn, notifyFn NotifyFn) *Spawner {
	return &Spawner{
		pool:     pool,
		toolsFn:  toolsFn,
		notifyFn: notifyFn,
	}
}

// Spawn creates and registers a sub-agent with the given parameters.
// It applies the agent-identity suffix to SystemPrompt and derives the parent ID
// from the "/" convention in req.ID (e.g. "main/coder" → parent "main").
// If req.InitialPrompt is non-empty, the agent's first turn is started immediately.
// If req.ThinkingBudget > 0, Anthropic extended-thinking provider options are applied.
func (s *Spawner) Spawn(ctx context.Context, req SpawnRequest) error {
	if s.pool == nil {
		return fmt.Errorf("no agent pool")
	}

	lm, err := s.pool.LanguageModelForModel(ctx, req.ModelName)
	if err != nil {
		return fmt.Errorf("spawn agent %q: get model %q: %w", req.ID, req.ModelName, err)
	}

	fullSystemPrompt := req.SystemPrompt
	if fullSystemPrompt != "" {
		fullSystemPrompt += "\n\n"
	}
	fullSystemPrompt += "## Your Agent Identity\nYour agent ID is: " + req.ID +
		"\nTo report results back to the orchestrator, call send_message with agent_id=\"main\"."

	// Derive parent ID: "main/coder" → "main"; "main/team/worker" → "main/team"; "toplevel" → "".
	parentID := ""
	if slash := strings.LastIndex(req.ID, "/"); slash > 0 {
		parentID = req.ID[:slash]
	}

	opts := SpawnOpts{
		SystemPrompt:   fullSystemPrompt,
		Name:           req.Name,
		NotifyParentID: parentID,
		TurnTimeout:    -1,
	}

	if req.ThinkingBudget > 0 {
		opts = s.applyThinkingBudget(opts, req.ThinkingBudget)
	}

	a, err := s.pool.Spawn(req.ID, lm, opts)
	if err != nil {
		return fmt.Errorf("spawn agent %q: %w", req.ID, err)
	}

	// Sub-agent tokens are NOT routed to the main chat.
	a.SetOnToken(func(_ string) {})

	subID := req.ID
	notifyFn := s.notifyFn
	pool := s.pool
	a.SetOnDone(func(e error) {
		if e == nil {
			return
		}
		slog.Error("sub-agent error", "agent", subID, "err", e)
		if notifyFn != nil {
			notifyFn(fmt.Sprintf("sub-agent %s: %v", subID, e))
		}
		// Surface the error to the main agent so the orchestrator can react.
		if main := pool.Get("main"); main != nil {
			msg := fmt.Sprintf("[sub-agent '%s' failed: %v — you should handle this or try a different approach]", subID, e)
			_ = pool.Send("main", msg)
		}
	})

	agentID := req.ID
	toolsFn := s.toolsFn
	a.SetToolsFn(func() []fantasy.AgentTool {
		if toolsFn == nil {
			return nil
		}
		return toolsFn(agentID)
	})
	a.SetOnToolCall(func(_, _, _ string) {}) // sub-agent tool calls are silent

	if req.InitialPrompt != "" {
		if err := pool.Send(req.ID, req.InitialPrompt); err != nil {
			slog.Warn("sub-agent: initial turn start failed", "agent", req.ID, "err", err)
		}
	}

	return nil
}

// applyThinkingBudget sets Anthropic extended-thinking options on opts.
// This is the only provider-specific code path in the spawner.
// The import of anthropicprovider in the agent package is intentional:
// the spawner owns provider-specific spawn option construction.
func (s *Spawner) applyThinkingBudget(opts SpawnOpts, budget int) SpawnOpts {
	opts.ProviderOptions = fantasy.ProviderOptions{
		anthropicprovider.Name: &anthropicprovider.ProviderOptions{
			Thinking: &anthropicprovider.ThinkingProviderOption{
				BudgetTokens: int64(budget),
			},
		},
	}
	return opts
}
