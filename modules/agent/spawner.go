package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

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
	// tokenFn, when set, receives every sub-agent's streamed text along with
	// the agent that produced it. Sub-agent output is otherwise discarded, so
	// this is what lets a focused view render a sub-agent's turn as it streams
	// rather than only after the turn completes.
	tokenFn func(agentID, text string)
	// tokenFlushFn, when set, delivers the tail a sub-agent's token batcher is
	// still holding. The batcher only sends when a token arrives ≥75ms after
	// the previous send, so a text segment's final tokens wait for the next
	// segment — or, for the last segment, forever. The harness flushes at
	// segment boundaries (tool-call dispatch) and turn end so focused
	// transcripts are never left one fragment short.
	tokenFlushFn func(agentID string)
	// promptFn, when set, receives each sub-agent's turn start (prompt plus any
	// queued inbox messages) with the producing agent. A focused transcript
	// needs this to show the user/broker prompt that began the turn.
	promptFn func(agentID, content string, queued bool)
	// steerFn, when set, receives each /steer message the spawner's agents
	// deliver mid-turn, so a focused transcript shows the guidance at the
	// point it entered the model's context.
	steerFn func(agentID, content string)
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

// SetTokenObserver installs an optional callback receiving each sub-agent's
// streamed text and the agent that produced it. The callback runs on the
// agent's turn goroutine, so it must be safe for concurrent use and must not
// block; the harness batches per agent before dispatching.
func (s *Spawner) SetTokenObserver(fn func(agentID, text string)) {
	s.tokenFn = fn
}

// SetTokenFlushObserver installs an optional callback that delivers the tail
// a sub-agent's token batcher is still holding (see tokenFlushFn). The spawner
// invokes it when a sub-agent dispatches a tool call and when its turn ends.
func (s *Spawner) SetTokenFlushObserver(fn func(agentID string)) {
	s.tokenFlushFn = fn
}

// SetPromptObserver installs an optional callback invoked when a sub-agent
// begins a turn, with the agent, the prompt text, and whether it came from the
// inbox rather than a direct send. Used to attribute transcript entries to the
// right agent; without it a focused view would have assistant text with no
// matching prompt.
func (s *Spawner) SetPromptObserver(fn func(agentID, content string, queued bool)) {
	s.promptFn = fn
}

// SetSteerObserver installs an optional callback invoked when a spawned agent
// delivers a /steer message into its running turn at a step boundary. The
// callback runs on the agent's turn goroutine and must not block: dispatches
// belong on their own goroutine (see dispatchAgentPrompt's deadlock notes).
func (s *Spawner) SetSteerObserver(fn func(agentID, content string)) {
	s.steerFn = fn
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

	// An explicit model always wins. An omitted model consults the pool's
	// sub-agent resolver, which the host uses to apply the configured working
	// model tier (possibly on a different provider).
	lm, modelName, err := s.pool.ResolveSubagentModel(ctx, req.ModelName, req.Endpoint)
	if err != nil {
		return fmt.Errorf("spawn agent %q: get model %q: %w", req.ID, req.ModelName, err)
	}
	contextWindow := s.pool.ContextWindowForModel(modelName)

	fullSystemPrompt := req.SystemPrompt
	if fullSystemPrompt != "" {
		fullSystemPrompt += "\n\n"
	}
	// The identity suffix is every sub-agent's shared ground truth: its ID, how
	// to report back, and how its queue behaves (messages sent mid-turn wait for
	// the next turn) plus how to cancel them via the queue extension's tools.
	fullSystemPrompt += "## Your Agent Identity\nYour agent ID is: " + req.ID +
		"\nTo report results back to the orchestrator, call send_message with agent_id=\"main\"." +
		"\n\nMessages sent to you while you are working are queued for your next turn. " +
		"If a queued message should not be acted on, cancel it: queue_peek() lists your pending messages, " +
		"queue_cancel() discards them all, or queue_cancel(index: N) cancels one. " +
		"A cancelled message cannot be recovered, so only cancel when the message is obsolete."

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

	// Sub-agent tokens are not routed to the main transcript, but an observer
	// can forward them (keyed by agent) so a focused view can render them.
	if s.tokenFn != nil {
		tokenFn := s.tokenFn
		subAgentID := req.ID
		a.SetOnToken(func(text string) { tokenFn(subAgentID, text) })
	} else {
		a.SetOnToken(func(_ string) {})
	}

	subID := req.ID
	notifyFn := s.notifyFn
	pool := s.pool
	tokenFlushFn := s.tokenFlushFn
	a.SetOnDone(func(e error) {
		// Turn end: the last text segment has no successor to flush its tail,
		// so flush explicitly or the final <75ms of sub-agent output never
		// reaches the focused transcript at all.
		if tokenFlushFn != nil {
			tokenFlushFn(subID)
		}
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
			msg, encodeErr := a.lifecycleMessage(
				lifecycleEventFailed,
				"child turn failed; inspect status and decide whether to retry or recover",
				e,
			)
			if encodeErr != nil {
				slog.Error("sub-agent: failed to encode error notification", "agent", subID, "err", encodeErr)
			} else if deliverErr := pool.Deliver(target, sdk.Message{
				Role:    sdk.RoleUser,
				Content: msg,
				Type:    sdk.MessageTypeProtocol,
			}, true); deliverErr != nil && !errors.Is(deliverErr, ErrAgentNotFound) {
				slog.Error("sub-agent: failed to notify creator of error", "agent", subID, "creator", target, "sendErr", deliverErr)
			}
		}
	})

	agentID := req.ID
	if s.promptFn != nil {
		promptFn := s.promptFn
		subID := req.ID
		a.SetOnTurnStart(func(content string, messages []sdk.Message) {
			for _, m := range messages {
				if m.Type == sdk.MessageTypeSystem ||
					m.Type == sdk.MessageTypeProtocol ||
					strings.TrimSpace(m.Content) == "" {
					continue
				}
				promptFn(subID, m.Content, true)
			}
			if strings.TrimSpace(content) != "" {
				promptFn(subID, content, false)
			}
		})
	}
	// Steer deliveries render like queued prompts at the moment they are
	// injected into the running turn — that is when the model will see them.
	if s.steerFn != nil {
		steerFn := s.steerFn
		subID := req.ID
		a.SetOnSteer(func(content string) {
			steerFn(subID, content)
		})
	}
	toolsFn := s.toolsFn
	a.SetToolsFn(func() []fantasy.AgentTool {
		if toolsFn == nil {
			return nil
		}
		return toolsFn(agentID)
	})
	toolCallFn := s.toolCallFn
	a.SetOnToolCall(func(id, toolName, input string) {
		// A dispatched tool call ends the preceding text segment; flush any
		// batched tail so the focused transcript shows the segment complete
		// before the tool activity appears.
		if tokenFlushFn != nil {
			tokenFlushFn(agentID)
		}
		if toolCallFn != nil {
			toolCallFn(agentID, id, toolName, input)
		}
	})

	if req.InitialPrompt != "" {
		// Start the first turn off this goroutine. Spawn runs inside the
		// spawning extension's WASM call, so any callback that fires while
		// starting a turn (onTurnStart, and anything the host does in response)
		// would re-enter that same extension and deadlock on its non-reentrant
		// call mutex. Detaching keeps turn start off the caller's stack.
		agentID := req.ID
		prompt := req.InitialPrompt
		go func() {
			if err := pool.Send(agentID, prompt); err != nil {
				slog.Warn("sub-agent: initial turn start failed", "agent", agentID, "err", err)
			}
		}()
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
