package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"charm.land/fantasy"
	anthropicprovider "charm.land/fantasy/providers/anthropic"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// Spawner creates sub-agents in a pool with appropriate callbacks and conventions.
// It encapsulates the parent-ID derivation, agent-identity system prompt suffix,
// and provider-option construction that was previously inline in harness/model.go SetProgram.
type Spawner struct {
	pool       *AgentPool
	toolsFn    func(agentID string) []fantasy.AgentTool
	notifyFn   func(text string)
	toolCallFn func(agentID, id, toolName, input string)
}

// NewSpawner creates a Spawner bound to the given pool.
// toolsFn is called on each sub-agent turn to get its tool list (may be nil).
// notifyFn is called when a sub-agent errors (may be nil).
func NewSpawner(
	pool *AgentPool,
	toolsFn func(agentID string) []fantasy.AgentTool,
	notifyFn func(text string),
) *Spawner {
	return &Spawner{
		pool:     pool,
		toolsFn:  toolsFn,
		notifyFn: notifyFn,
	}
}

// SetToolCallObserver installs an optional callback invoked when spawned
// sub-agents dispatch tool calls.
func (s *Spawner) SetToolCallObserver(fn func(agentID, id, toolName, input string)) {
	s.toolCallFn = fn
}

// Spawn creates and registers a sub-agent with the given parameters.
// It applies the agent-identity suffix to SystemPrompt and derives the parent ID
// from the "/" convention in req.ID (e.g. "main/coder" → parent "main").
// If req.InitialPrompt is non-empty, the agent's first turn is queued asynchronously
// via pool.Send (non-blocking; the turn runs in a goroutine).
// If req.ThinkingBudget > 0, Anthropic extended-thinking provider options are applied.
func (s *Spawner) Spawn(ctx context.Context, req extension.SpawnRequest) error {
	if s.pool == nil {
		return fmt.Errorf("no agent pool")
	}

	lm, err := s.pool.LanguageModelForModel(ctx, req.ModelName)
	if err != nil {
		return fmt.Errorf("spawn agent %q: get model %q: %w", req.ID, req.ModelName, err)
	}
	modelName := req.ModelName
	if modelName == "" {
		modelName = s.pool.DefaultModelName()
	}
	contextWindow := s.pool.ContextWindowForModel(modelName)

	fullSystemPrompt := req.SystemPrompt
	if fullSystemPrompt != "" {
		fullSystemPrompt += "\n\n"
	}
	fullSystemPrompt += "## Your Agent Identity\nYour agent ID is: " + req.ID +
		"\nTo report results back to the orchestrator, call send_message with agent_id=\"main\"."

	opts := SpawnOpts{
		SystemPrompt:  fullSystemPrompt,
		Name:          req.Name,
		ModelName:     modelName,
		ContextWindow: contextWindow,
		TurnTimeout:   -1,
	}

	if req.ThinkingBudget > 0 {
		opts = s.applyThinkingBudget(opts, req.ThinkingBudget)
	}

	a, err := s.pool.Spawn(req.ID, lm, opts)
	if err != nil {
		return fmt.Errorf("spawn agent %q: %w", req.ID, err)
	}
	// Set creatorID directly (not via SpawnOpts to avoid increasing SpawnOpts GC scan span).
	if req.CallerID != "" {
		a.creatorID = req.CallerID
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
		// Surface the error to the actual creator so nested agents are notified too.
		target := a.lifecycleTarget()
		if targetAgent := pool.Get(target); targetAgent != nil {
			msg, encodeErr := a.lifecycleMessage(lifecycleEventFailed, "child turn failed; inspect status and decide whether to retry or recover", e)
			if encodeErr != nil {
				slog.Error("sub-agent: failed to encode error notification", "agent", subID, "err", encodeErr)
			} else if deliverErr := pool.Deliver(target, sdk.Message{
				Role:    sdk.RoleUser,
				Content: msg,
			}, true); deliverErr != nil && !errors.Is(deliverErr, ErrAgentNotFound) {
				slog.Error("sub-agent: failed to notify creator of error", "agent", subID, "creator", target, "sendErr", deliverErr)
			}
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
	toolCallFn := s.toolCallFn
	a.SetOnToolCall(func(id, toolName, input string) {
		if toolCallFn != nil {
			toolCallFn(agentID, id, toolName, input)
		}
	})

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
