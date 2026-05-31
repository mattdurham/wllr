package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

// Agent wraps a fantasy.LanguageModel with a message inbox and lifecycle management.
// Each agent maintains its own conversation history and can run one turn at a time.
type Agent struct {
	lm   fantasy.LanguageModel
	pool *AgentPool

	cancel context.CancelFunc

	onToken func(token string)

	onDone func(err error)

	onToolCallFn func(id, toolName, input string)

	toolsFn func() []fantasy.AgentTool

	id           string
	name         string
	modelName    string // for context window lookup
	systemPrompt string
	creatorID    string // ID of the agent that spawned this one; "" for top-level agents

	// lastSummary is the most recent compaction summary text. Passed to
	// compactHistory as priorSummary on subsequent compaction calls so the model
	// can build an incremental summary. Protected by lastSummaryMu.
	lastSummary string
	opts        SpawnOpts
	inbox       []sdk.Message

	history []sdk.Message

	// onToken is called per text delta. Set via SetOnToken before calling Submit.
	onTokenMu sync.RWMutex

	// onDone is called when a turn completes (with nil or an error). Set via SetOnDone.
	onDoneMu sync.RWMutex

	// onToolCall is called when a tool call is dispatched. Set via SetOnToolCall.
	onToolCallMu sync.RWMutex

	// toolsFn, if set, is called on each Submit to get the current tool list.
	// Takes priority over opts.Tools; allows dynamic tool registration.
	toolsFnMu sync.RWMutex

	// systemPrompt overrides opts.SystemPrompt when non-empty.
	// Set via SetSystemPrompt; safe to call before the first Submit.
	systemPromptMu sync.RWMutex

	lastSummaryMu sync.RWMutex

	// inbox holds messages injected between turns via AppendInbox.
	inboxMu sync.RWMutex

	// pendingShutdownFrom is set when a shutdown_request arrives alongside normal
	// pending messages in finishTurn. The shutdown is deferred until all normal
	// messages are drained (drain-until-empty pattern). Only accessed from
	// finishTurn, which runs after isRunning transitions — no separate lock needed.
	pendingShutdownFrom string

	// cancelMu protects the cancel function for the current active turn.
	cancelMu sync.Mutex

	// isRunning is set to true while Submit's goroutine is active. A second
	// Submit call that arrives while a turn is running appends content to the
	// inbox instead of starting a new goroutine. The running goroutine drains
	// inbox on completion (drain-until-empty pattern). See NOTES.md §17.
	isRunning atomic.Bool

	// history is the conversation history for this agent (all completed turns).
	historyMu sync.Mutex
}

// SetOnToken sets the callback invoked for each text delta during streaming.
// Thread-safe; may be called before each Submit.
func (a *Agent) SetOnToken(fn func(token string)) {
	a.onTokenMu.Lock()
	a.onToken = fn
	a.onTokenMu.Unlock()
}

// SetOnDone sets the callback invoked when a turn finishes (err may be nil).
// Thread-safe; may be called before each Submit.
func (a *Agent) SetOnDone(fn func(err error)) {
	a.onDoneMu.Lock()
	a.onDone = fn
	a.onDoneMu.Unlock()
}

// SetSystemPrompt sets the system prompt for all subsequent turns, overriding
// the value in SpawnOpts. Thread-safe; safe to call before the first Submit.
func (a *Agent) SetSystemPrompt(prompt string) {
	a.systemPromptMu.Lock()
	a.systemPrompt = prompt
	a.systemPromptMu.Unlock()
}

// AppendSystemPrompt appends text to the current system prompt with a blank
// line separator. Thread-safe; safe to call before the first Submit.
func (a *Agent) AppendSystemPrompt(text string) {
	a.systemPromptMu.Lock()
	if a.systemPrompt == "" {
		a.systemPrompt = text
	} else {
		a.systemPrompt += "\n\n" + text
	}
	a.systemPromptMu.Unlock()
}

// SetOnToolCall sets the callback invoked when the agent dispatches a tool call.
// The callback receives the tool call ID, tool name, and JSON input string.
// Thread-safe; may be called before each Submit.
func (a *Agent) SetOnToolCall(fn func(id, toolName, input string)) {
	a.onToolCallMu.Lock()
	a.onToolCallFn = fn
	a.onToolCallMu.Unlock()
}

// SetToolsFn sets a function called on each Submit to get the current tool list.
// When set, it takes priority over opts.Tools. Thread-safe.
func (a *Agent) SetToolsFn(fn func() []fantasy.AgentTool) {
	a.toolsFnMu.Lock()
	a.toolsFn = fn
	a.toolsFnMu.Unlock()
}

// AppendInbox adds msg to the agent's pending inbox.
// Messages are delivered before the next Submit turn via DrainInbox.
// Thread-safe. Silently drops messages with empty content — empty content
// causes Anthropic API rejection ("text content blocks must be non-empty").
func (a *Agent) AppendInbox(msg sdk.Message) {
	if strings.TrimSpace(msg.Content) == "" {
		slog.Warn("agent: dropping inbox message with empty content", "agent", a.id, "role", msg.Role)
		return
	}
	a.inboxMu.Lock()
	a.inbox = append(a.inbox, msg)
	a.inboxMu.Unlock()
}

// DrainInbox atomically returns all pending inbox messages and clears the inbox.
// Thread-safe.
func (a *Agent) DrainInbox() []sdk.Message {
	a.inboxMu.Lock()
	msgs := a.inbox
	a.inbox = nil
	a.inboxMu.Unlock()
	return msgs
}

// InboxLen returns the number of messages currently queued in the agent's inbox.
// Thread-safe. Does not drain or modify the inbox. Useful for idle detection in
// coordination tools without consuming pending messages.
func (a *Agent) InboxLen() int {
	a.inboxMu.RLock()
	n := len(a.inbox)
	a.inboxMu.RUnlock()
	return n
}

// ModelName returns the model name used for context-window sizing.
func (a *Agent) ModelName() string { return a.modelName }

// SystemPrompt returns the agent's current effective system prompt.
func (a *Agent) SystemPrompt() string {
	a.systemPromptMu.RLock()
	base := a.systemPrompt
	a.systemPromptMu.RUnlock()
	specific := a.opts.SystemPrompt
	if base != "" && specific != "" {
		return base + "\n\n" + specific
	}
	if base != "" {
		return base
	}
	return specific
}

// History returns a snapshot of the agent's conversation history.
func (a *Agent) History() []sdk.Message {
	a.historyMu.Lock()
	h := make([]sdk.Message, len(a.history))
	copy(h, a.history)
	a.historyMu.Unlock()
	return h
}

// LastSummary returns the most recent compaction summary.
func (a *Agent) LastSummary() string {
	a.lastSummaryMu.RLock()
	defer a.lastSummaryMu.RUnlock()
	return a.lastSummary
}

// SetLastSummary sets the compaction summary (used in tests).
func (a *Agent) SetLastSummary(s string) {
	a.lastSummaryMu.Lock()
	a.lastSummary = s
	a.lastSummaryMu.Unlock()
}

// ID returns the agent's unique identifier within its pool.
func (a *Agent) ID() string { return a.id }

// Name returns the agent's human-readable display name.
func (a *Agent) Name() string { return a.name }

// IsRunning reports whether the agent is currently mid-turn.
func (a *Agent) IsRunning() bool { return a.isRunning.Load() }

// CreatorID returns the ID of the agent that spawned this agent.
// Returns an empty string for top-level agents that were not spawned by another agent.
func (a *Agent) CreatorID() string { return a.creatorID }

// Cancel cancels the current active turn, if any.
// No-op if no turn is running.
func (a *Agent) Cancel() {
	a.cancelMu.Lock()
	fn := a.cancel
	a.cancelMu.Unlock()
	if fn != nil {
		fn()
	}
}

// Submit starts a new turn with the given user content.
// It injects any pending inbox messages into the conversation history before
// constructing the request. Calls onToken for each text delta and onDone when
// the turn completes. Non-blocking: the fantasy.Agent.Stream call runs in a
// goroutine; results are delivered via the registered callbacks.
//
// Note: Submit creates a new fantasy.Agent per turn (matching the established
// startStream pattern). This is intentional — fantasy.Agent may hold internal
// state that must be reset each turn.
func (a *Agent) Submit(ctx context.Context, content string) {
	// Drain any inbox messages queued by other agents since the last turn.
	inboxMsgs := a.DrainInbox()

	// Guard against concurrent Submit calls. If a turn is already running,
	// queue content to inbox and return. The goroutine's post-turn drain loop
	// will process queued messages before declaring the turn done.
	if !a.isRunning.CompareAndSwap(false, true) {
		// Re-queue previously-drained messages first (FIFO order), then new content.
		for _, msg := range inboxMsgs {
			a.AppendInbox(msg)
		}
		if content != "" {
			a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: content})
		}
		return
	}

	// Snapshot current history under lock.
	a.historyMu.Lock()
	priorHistory := make([]sdk.Message, len(a.history))
	copy(priorHistory, a.history)
	a.historyMu.Unlock()
	// Append inbox messages after prior history so they are the most-recent context.
	// This ensures the last message is always a user/inbox message when inbox is non-empty,
	// making empty-prompt valid. See NOTES.md §16.
	if len(inboxMsgs) > 0 {
		combined := make([]sdk.Message, 0, len(priorHistory)+len(inboxMsgs))
		combined = append(combined, priorHistory...)
		combined = append(combined, inboxMsgs...)
		priorHistory = combined
	}

	// Create a child context with a per-turn timeout.
	// Default is 30 minutes (pi.dev uses no timeout; 10 minutes was too short for heavy tool use).
	// Set SpawnOpts.TurnTimeout to override; negative value disables entirely.
	turnTimeout := 30 * time.Minute
	if a.opts.TurnTimeout < 0 {
		turnTimeout = 0
	} else if a.opts.TurnTimeout > 0 {
		turnTimeout = a.opts.TurnTimeout
	}
	var childCtx context.Context
	var cancel context.CancelFunc
	if turnTimeout > 0 {
		childCtx, cancel = context.WithTimeout(ctx, turnTimeout)
	} else {
		childCtx, cancel = context.WithCancel(ctx)
	}
	a.cancelMu.Lock()
	a.cancel = cancel
	a.cancelMu.Unlock()

	// Capture callbacks and pool reference now (avoid holding locks in goroutine).
	a.onTokenMu.RLock()
	onToken := a.onToken
	a.onTokenMu.RUnlock()

	a.onDoneMu.RLock()
	onDone := a.onDone
	a.onDoneMu.RUnlock()

	a.toolsFnMu.RLock()
	toolsFn := a.toolsFn
	a.toolsFnMu.RUnlock()

	a.onToolCallMu.RLock()
	onToolCall := a.onToolCallFn
	a.onToolCallMu.RUnlock()

	// Capture last summary for iterative compaction (read before goroutine launch
	// so the goroutine uses a consistent snapshot).
	a.lastSummaryMu.RLock()
	priorSummary := a.lastSummary
	a.lastSummaryMu.RUnlock()

	pool := a.pool
	lm := a.lm
	opts := a.opts

	go func() {
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				if onDone != nil {
					onDone(fmt.Errorf("agent %s: panic: %v", a.id, r))
				}
			}
		}()

		if lm == nil {
			if onDone != nil {
				onDone(fmt.Errorf("agent %s: no language model configured", a.id))
			}
			return
		}

		// Resolve tools: prefer dynamic toolsFn, fall back to opts.Tools.
		tools := opts.Tools
		if toolsFn != nil {
			tools = toolsFn()
		}

		// Combine base (dynamic) and agent-specific (SpawnOpts) system prompts.
		a.systemPromptMu.RLock()
		base := a.systemPrompt
		a.systemPromptMu.RUnlock()
		specific := opts.SystemPrompt
		var sysPrompt string
		switch {
		case base != "" && specific != "":
			sysPrompt = base + "\n\n" + specific
		case base != "":
			sysPrompt = base
		default:
			sysPrompt = specific
		}

		// Build fantasy agent options.
		var agentOpts []fantasy.AgentOption
		if len(tools) > 0 {
			agentOpts = append(agentOpts, fantasy.WithTools(tools...))
		}
		if sysPrompt != "" {
			agentOpts = append(agentOpts, fantasy.WithSystemPrompt(sysPrompt))
		}

		if len(opts.ProviderOptions) > 0 {
			agentOpts = append(agentOpts, fantasy.WithProviderOptions(opts.ProviderOptions))
		}

		agentOpts = append(agentOpts, fantasy.WithMaxRetries(6))
		fa := fantasy.NewAgent(lm, agentOpts...)

		// Proactive compaction: if the estimated context is close to the model's
		// limit, summarize old history BEFORE sending, avoiding a 400 error.
		// Use pool-configured context window if set; fall back to model-name lookup.
		contextWindow := int64(0)
		if pool != nil {
			contextWindow = pool.ContextWindow()
		}
		if contextWindow == 0 {
			contextWindow = contextWindowForModel(a.modelName)
		}
		history := priorHistory
		if shouldCompact(history, sysPrompt, content, contextWindow) {
			if onToken != nil {
				onToken("[Compacting context…]\n\n")
			}
			// Keep 10% of the model's context window as recent history.
			// This scales correctly: a 1M-token model keeps 100k, a 200k model keeps 20k.
			keepRecent := contextWindow / 10
			if keepRecent <= 0 {
				keepRecent = defaultKeepRecentTokens
			}
			compacted, summaryText, cerr := compactHistory(childCtx, lm, history, priorSummary, keepRecent)
			if cerr == nil {
				history = compacted
				a.historyMu.Lock()
				a.history = compacted
				a.historyMu.Unlock()
				if summaryText != "" {
					a.lastSummaryMu.Lock()
					a.lastSummary = summaryText
					a.lastSummaryMu.Unlock()
				}
			}
		}

		collectedText, err := streamTurn(childCtx, fa, history, content, pool, onToken, onToolCall)

		// Reactive fallback: if we still hit a context error, trim and retry once.
		if err != nil && isContextTooLong(err) {
			if onToken != nil {
				onToken("\n\n[Still too long — trimming and retrying…]\n\n")
			}
			if len(history) > keepMessages {
				history = history[len(history)-keepMessages:]
				a.historyMu.Lock()
				a.history = history
				a.historyMu.Unlock()
			}
			collectedText, err = streamTurn(childCtx, fa, history, content, pool, onToken, onToolCall)
		}

		// Always record what the user said and the assistant response.
		// Providers require strictly alternating user/assistant messages —
		// leaving history ending with a lone user message causes "text content
		// cannot be empty" on the next turn.
		assistantText := collectedText
		if assistantText == "" {
			if childCtx.Err() != nil {
				assistantText = "[response cancelled]"
			} else {
				// Tool-only turn: no text produced. Use a placeholder so the
				// user message is never silently dropped from history.
				assistantText = "[tool calls only]"
			}
		}
		// Record this turn to history. When content is empty (drain-until-empty
		// path), use the inbox messages instead so we never write an empty user
		// message — Anthropic rejects empty text content blocks.
		a.historyMu.Lock()
		if content != "" {
			a.history = append(a.history, sdk.Message{Role: sdk.RoleUser, Content: content})
		} else {
			for _, m := range inboxMsgs {
				// System messages are Go-level control messages — never record them
				// in history so they cannot leak into future LLM context.
				if m.Type != sdk.MessageTypeSystem {
					a.history = append(a.history, m)
				}
			}
		}
		a.history = append(a.history, sdk.Message{Role: sdk.RoleAssistant, Content: assistantText})
		a.historyMu.Unlock()

		a.finishTurn(ctx, err, childCtx.Err(), onDone)
	}()
}

// finishTurn releases isRunning and either fires onDone or starts the next drain turn.
// Extracted from Submit to keep its cyclomatic complexity below threshold.
// Implements the drain-until-empty pattern: if new inbox messages arrived while a turn was
// running, re-queue them and start another turn without a race window. See NOTES.md §17.
//
// Graceful shutdown: if a system shutdown_request message is found in pending messages,
// it is separated from normal messages. If normal messages coexist, they are processed
// first (drain-until-empty) and the shutdown is deferred via pendingShutdownFrom. When
// only the shutdown_request remains (no normal pending messages), finishTurn sends an
// AGENT_SHUTDOWN system message to the creator, removes the agent from the pool, and
// fires onDone exactly once.
//
// ctx is the original context passed to Submit — it is threaded into drain turns so
// that harness shutdown can cancel in-flight drain turns rather than running them to
// their 30-minute timeout.
func (a *Agent) finishTurn(ctx context.Context, err error, ctxErr error, onDone func(error)) {
	a.isRunning.Store(false)

	// Only drain on successful turns — errors and cancellations terminate the chain.
	if err == nil && ctxErr == nil {
		pending := a.DrainInbox()

		// Scan new pending messages for a system shutdown_request.
		// Any other system messages (non-shutdown_request) fall through to normalPending.
		var shutdownFrom string
		var normalPending []sdk.Message
		for _, m := range pending {
			if m.Type == sdk.MessageTypeSystem {
				var evt struct {
					Event string `json:"event"`
					From  string `json:"from"`
				}
				if json.Unmarshal([]byte(m.Content), &evt) == nil &&
					evt.Event == "shutdown_request" {
					shutdownFrom = evt.From
					continue // consume; do not re-queue as a message
				}
			}
			normalPending = append(normalPending, m)
		}

		// Check for a deferred shutdown_request from a previous finishTurn cycle.
		// invariant: pendingShutdownFrom is only written and read from finishTurn,
		// which runs after isRunning transitions to false. Submit must never access
		// this field.
		if shutdownFrom == "" {
			shutdownFrom = a.pendingShutdownFrom
		}

		if len(normalPending) > 0 {
			// Normal messages remain: re-queue them for the next drain turn.
			// Defer the shutdown (if any) via pendingShutdownFrom — stored on the
			// Agent rather than re-injected into the inbox so it survives Submit's
			// initial DrainInbox without being consumed as an inbox message.
			for _, m := range normalPending {
				a.AppendInbox(m)
			}
			// invariant: pendingShutdownFrom is only written and read from finishTurn,
		// which runs after isRunning transitions to false. Submit must never access
		// this field.
		a.pendingShutdownFrom = shutdownFrom // "" if no shutdown pending
			// Do NOT fire onDone here: the drain sub-turn's finishTurn will fire it
			// when the inbox is finally empty, preventing a double StreamDoneMsg.
			// Pass the original ctx so harness shutdown can cancel drain turns.
			a.Submit(ctx, "")
			return
		}

		// No normal pending messages remain. Handle deferred shutdown if present.
		if shutdownFrom != "" {
			// invariant: pendingShutdownFrom is only written and read from finishTurn,
			// which runs after isRunning transitions to false. Submit must never access
			// this field.
			a.pendingShutdownFrom = "" // clear deferred state

			// Send AGENT_SHUTDOWN back to the creator, remove self from pool, and
			// fire onDone exactly once before returning.
			shutdownPayload, _ := json.Marshal(map[string]string{
				"event":    "AGENT_SHUTDOWN",
				"agent_id": a.id,
			})
			if a.pool != nil {
				if err := a.pool.SendMessage(shutdownFrom, sdk.Message{
					Role:    sdk.RoleUser,
					Content: string(shutdownPayload),
					Type:    sdk.MessageTypeSystem,
				}); err != nil && !errors.Is(err, ErrAgentNotFound) {
					slog.Warn("finishTurn: failed to send AGENT_SHUTDOWN", "agent", a.id, "err", err)
				}
				if err := a.pool.Close(a.id); err != nil && !errors.Is(err, ErrAgentNotFound) {
					slog.Warn("finishTurn: failed to close self", "agent", a.id, "err", err)
				}
			}
			if onDone != nil {
				onDone(nil)
			}
			return
		}
	}

	if onDone != nil {
		onDone(err)
	}
}

// streamTurn sends history+content to fa and collects the full text response.
// Extracted from Submit to keep its cyclomatic complexity below threshold.
func streamTurn(
	ctx context.Context,
	fa fantasy.Agent,
	history []sdk.Message,
	content string,
	pool *AgentPool,
	onToken func(string),
	onToolCall func(id, name, input string),
) (string, error) {
	var collected string
	_, err := fa.Stream(ctx, fantasy.AgentStreamCall{
		Messages: sdkToFantasyMessages(history),
		Prompt:   content,
		OnTextDelta: func(_, text string) error {
			if text == "" {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			collected += text
			if pool != nil {
				pool.addTokens(1)
			}
			if onToken != nil {
				onToken(text)
			}
			return nil
		},
		OnToolCall: func(toolCall fantasy.ToolCallContent) error {
			if onToolCall != nil && !toolCall.ProviderExecuted {
				onToolCall(toolCall.ToolCallID, toolCall.ToolName, toolCall.Input)
			}
			return nil
		},
	})
	return collected, err
}

// isContextTooLong returns true when the API rejected the request because the
// prompt exceeded the model's context window.
func isContextTooLong(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "prompt is too long") ||
		strings.Contains(s, "context_length_exceeded") ||
		strings.Contains(s, "maximum context length") ||
		strings.Contains(s, "tokens > ") // Anthropic: "213370 tokens > 200000 maximum"
}

// sdkToFantasyMessages converts sdk.Message history to fantasy.Message format.
// Only user and assistant roles are included; other roles are skipped.
// System and steering messages are consumed by the Go runtime and must never
// reach the LLM context — they are filtered here.
func sdkToFantasyMessages(msgs []sdk.Message) []fantasy.Message {
	result := make([]fantasy.Message, 0, len(msgs))
	for _, m := range msgs {
		// System and steering messages are handled by Go, never sent to the LLM.
		if m.Type == sdk.MessageTypeSystem || m.Type == sdk.MessageTypeSteering {
			continue
		}
		var role fantasy.MessageRole
		switch m.Role {
		case sdk.RoleUser:
			role = fantasy.MessageRoleUser
		case sdk.RoleAssistant:
			role = fantasy.MessageRoleAssistant
		default:
			continue
		}
		result = append(result, fantasy.Message{
			Role:    role,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: m.Content}},
		})
	}
	return result
}
