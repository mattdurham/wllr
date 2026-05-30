package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
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

	id             string
	name           string
	modelName      string // for context window lookup
	systemPrompt   string
	notifyParentID string // if set, pool sends a completion message here after the final turn

	// lastSummary is the most recent compaction summary text. Passed to
	// compactHistory as priorSummary on subsequent compaction calls so the model
	// can build an incremental summary. Protected by lastSummaryMu.
	lastSummary string

	opts  SpawnOpts
	inbox []sdk.Message

	history []sdk.Message

	// lastUsage is the token usage from the most recently completed turn.
	// Zero-valued before the first turn or when the last turn returned an error.
	// Protected by lastUsageMu.
	lastUsage   fantasy.Usage
	lastUsageMu sync.RWMutex

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
	inboxMu sync.Mutex

	// cancelMu protects the cancel function for the current active turn.
	cancelMu sync.Mutex

	// history is the conversation history for this agent (all completed turns).
	historyMu sync.Mutex

	// isRunning is set to true while Submit's goroutine is active. A second
	// Submit call that arrives while a turn is running appends content to the
	// inbox instead of starting a new goroutine. The running goroutine drains
	// inbox on completion (drain-until-empty pattern). See NOTES.md §17.
	isRunning atomic.Bool
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

// LastUsage returns the token usage from the most recently completed turn.
// The returned value is zero-valued before the first turn or when the last
// turn returned an error.
func (a *Agent) LastUsage() fantasy.Usage {
	a.lastUsageMu.RLock()
	defer a.lastUsageMu.RUnlock()
	return a.lastUsage
}

// setLastUsage stores the usage from the most recently completed turn.
func (a *Agent) setLastUsage(u fantasy.Usage) {
	a.lastUsageMu.Lock()
	a.lastUsage = u
	a.lastUsageMu.Unlock()
}

// ID returns the agent's unique identifier within its pool.
func (a *Agent) ID() string { return a.id }

// Name returns the agent's human-readable display name.
func (a *Agent) Name() string { return a.name }

// IsRunning reports whether the agent is currently mid-turn.
func (a *Agent) IsRunning() bool { return a.isRunning.Load() }

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

		// Build the fantasy agent with resolved tools and system prompt.
		sysPrompt, fa := a.buildFantasyAgent(lm, opts, toolsFn)

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
		// compactedThisTurn is set true when compaction runs successfully this turn.
		// It is passed to the EventContextUsage payload so extensions can react.
		compactedThisTurn := false
		if shouldRunProactiveCompaction(a, pool, history, sysPrompt, content, contextWindow) {
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
				compactedThisTurn = true
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

		collectedText, usage, err := streamTurn(childCtx, fa, history, content, pool, onToken, onToolCall)

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
			collectedText, usage, err = streamTurn(childCtx, fa, history, content, pool, onToken, onToolCall)
		}

		// Store the real usage from this turn (zero-valued on error).
		a.setLastUsage(usage)

		// Dispatch EventContextUsage after each successful turn so extensions and the
		// harness can track real context window consumption. Only fires on success —
		// matches the existing pattern where events are not dispatched on error.
		if err == nil && pool != nil {
			cu := sdk.ContextUsageFromFantasy(usage, contextWindow)
			pool.dispatchContextUsage(cu, compactedThisTurn)
		}

		// Record this turn to history and complete the turn lifecycle.
		a.recordTurnHistory(content, collectedText, inboxMsgs, childCtx.Err())
		a.finishTurn(err, childCtx.Err(), onDone)
	}()
}

// buildFantasyAgent resolves tools and system prompt, then creates and returns
// the fantasy.Agent for the current turn along with the effective system prompt string.
// Extracted from Submit to reduce its cyclomatic complexity.
func (a *Agent) buildFantasyAgent(
	lm fantasy.LanguageModel,
	opts SpawnOpts,
	toolsFn func() []fantasy.AgentTool,
) (sysPrompt string, fa fantasy.Agent) {
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
	fa = fantasy.NewAgent(lm, agentOpts...)
	return sysPrompt, fa
}

// recordTurnHistory appends the user message and assistant response to the agent's
// conversation history. Extracted from Submit to reduce its cyclomatic complexity.
//
//   - content is the user prompt (may be empty on drain-only turns).
//   - collectedText is the raw assistant response text from the turn.
//   - inboxMsgs are the inbox messages delivered at turn start.
//   - ctxErr is childCtx.Err(), used to determine cancel-placeholder text.
func (a *Agent) recordTurnHistory(content, collectedText string, inboxMsgs []sdk.Message, ctxErr error) {
	// Providers require strictly alternating user/assistant messages.
	// A tool-only or cancelled turn produces no text — use a placeholder so the
	// user message is never silently dropped from history.
	assistantText := collectedText
	if assistantText == "" {
		if ctxErr != nil {
			assistantText = "[response cancelled]"
		} else {
			assistantText = "[tool calls only]"
		}
	}
	// When content is empty (drain-until-empty path), use the inbox messages so
	// we never write an empty user message — Anthropic rejects empty text content.
	a.historyMu.Lock()
	if content != "" {
		a.history = append(a.history, sdk.Message{Role: sdk.RoleUser, Content: content})
	} else {
		a.history = append(a.history, inboxMsgs...)
	}
	a.history = append(a.history, sdk.Message{Role: sdk.RoleAssistant, Content: assistantText})
	a.historyMu.Unlock()
}

// finishTurn releases isRunning and either fires onDone or starts the next drain turn.
// Extracted from Submit to keep its cyclomatic complexity below threshold.
// Implements the drain-until-empty pattern: if new inbox messages arrived while a turn was
// running, re-queue them and start another turn without a race window. See NOTES.md §17.
func (a *Agent) finishTurn(err error, ctxErr error, onDone func(error)) {
	a.isRunning.Store(false)

	// Only drain on successful turns — errors and cancellations terminate the chain.
	if err == nil && ctxErr == nil {
		pending := a.DrainInbox()
		if len(pending) > 0 {
			for _, m := range pending {
				a.AppendInbox(m)
			}
			// Do NOT fire onDone here: the drain sub-turn's finishTurn will fire it
			// when the inbox is finally empty, preventing a double StreamDoneMsg.
			a.Submit(context.Background(), "")
			return
		}
	}

	// Notify parent if configured — gives the orchestrator a guaranteed wakeup.
	// Pass the notification as the prompt content (not via SendMessage+empty Send)
	// so pool.Send records a non-empty user message in the parent's history.
	if a.notifyParentID != "" && err == nil && ctxErr == nil {
		notification := "[from agent '" + a.name + "' (" + a.id + ")]: turn complete — call get_agent_status(\"" + a.id + "\", 20) to read results"
		_ = a.pool.Send(a.notifyParentID, notification)
	}

	if onDone != nil {
		onDone(err)
	}
}

// shouldRunProactiveCompaction returns true when proactive context compaction
// should run before the next API call. It first checks the percentage-based
// trigger (real API usage vs configured threshold) and falls back to the
// chars/4 heuristic when no usage data exists (first turn) or when the pool
// has not configured percentage-based compaction.
func shouldRunProactiveCompaction(a *Agent, pool *AgentPool, history []sdk.Message, sysPrompt, content string, contextWindow int64) bool {
	if pool != nil {
		cfg := pool.CompactConfig()
		if cfg.Enabled && shouldCompactByUsage(a.LastUsage(), contextWindow, cfg.ThresholdPct) {
			return true
		}
	}
	return shouldCompact(history, sysPrompt, content, contextWindow)
}

// streamTurn sends history+content to fa and collects the full text response.
// It returns the collected text, the token usage reported by the provider, and
// any error. On error, the returned Usage is zero-valued.
// Extracted from Submit to keep its cyclomatic complexity below threshold.
func streamTurn(
	ctx context.Context,
	fa fantasy.Agent,
	history []sdk.Message,
	content string,
	pool *AgentPool,
	onToken func(string),
	onToolCall func(id, name, input string),
) (string, fantasy.Usage, error) {
	var collected string
	result, err := fa.Stream(ctx, fantasy.AgentStreamCall{
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
	if err != nil {
		return collected, fantasy.Usage{}, err
	}
	var usage fantasy.Usage
	if result != nil {
		usage = result.TotalUsage
	}
	return collected, usage, nil
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
func sdkToFantasyMessages(msgs []sdk.Message) []fantasy.Message {
	result := make([]fantasy.Message, 0, len(msgs))
	for _, m := range msgs {
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
