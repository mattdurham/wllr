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

	// pendingShutdownFrom is set when a shutdown_request arrives alongside normal
	// pending messages in finishTurn. The shutdown is deferred until all normal
	// messages are drained (drain-until-empty pattern). Only accessed from
	// finishTurn, which runs after isRunning transitions — no separate lock needed.
	pendingShutdownFrom string

	// inbox is the agent's pending-message queue (see mailbox). It owns its own
	// mutex; the agent does not lock it directly.
	inbox mailbox

	history []sdk.Message

	opts SpawnOpts

	// lastUsage is the token usage from the most recently completed turn.
	// Updated after each turn by setLastUsage. Read by LastUsage().
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

	// lmMu guards lm and modelName, which can be swapped at runtime via SetModel
	// (e.g. the /model picker). Submit reads them under this lock.
	lmMu sync.RWMutex

	lastSummaryMu sync.RWMutex

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
	a.inbox.append(a.id, msg)
}

// DrainInbox atomically returns all pending inbox messages and clears the inbox.
// Thread-safe.
func (a *Agent) DrainInbox() []sdk.Message {
	return a.inbox.drain()
}

// InboxLen returns the number of messages currently queued in the agent's inbox.
// Thread-safe. Does not drain or modify the inbox. Useful for idle detection in
// coordination tools without consuming pending messages.
func (a *Agent) InboxLen() int {
	return a.inbox.len()
}

// ModelName returns the model name used for context-window sizing.
func (a *Agent) ModelName() string {
	a.lmMu.RLock()
	defer a.lmMu.RUnlock()
	return a.modelName
}

// SetModel swaps the language model and model name used for subsequent turns.
// Thread-safe; a turn already in flight finishes on the previous model, and the
// next Submit picks up the new one. Used by the /model picker to switch the
// active model at runtime.
func (a *Agent) SetModel(lm fantasy.LanguageModel, modelName string) {
	a.lmMu.Lock()
	a.lm = lm
	a.modelName = modelName
	a.lmMu.Unlock()
}

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
// Returns a zero-valued Usage before the first turn completes.
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

// CreatorID returns the ID of the agent that spawned this agent.
// Returns an empty string for top-level agents that were not spawned by another agent.
func (a *Agent) CreatorID() string { return a.creatorID }

// SetCreatorID sets the ID of the agent that spawned this agent. Normally set by
// Spawner.Spawn immediately after pool.Spawn; exposed for tests and for callers
// that wire the creator relationship outside the spawner. Must be called before
// the agent's first turn completes so the idle-notification path can use it.
func (a *Agent) SetCreatorID(id string) { a.creatorID = id }

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
	a.lmMu.RLock()
	lm := a.lm
	a.lmMu.RUnlock()
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
		a.executeTurn(ctx, childCtx, content, inboxMsgs, priorHistory, priorSummary, lm, opts, pool, toolsFn, onToken, onToolCall, onDone)
	}()
}

// executeTurn contains the core LLM interaction logic for a single turn.
// Extracted from Submit to keep Submit's cyclomatic complexity below threshold.
// It builds the fantasy agent, performs optional proactive compaction, streams
// the turn, handles reactive fallback on context-too-long errors, records
// history, and delegates to finishTurn for drain-until-empty / shutdown logic.
func (a *Agent) executeTurn(
	ctx context.Context,
	childCtx context.Context,
	content string,
	inboxMsgs []sdk.Message,
	priorHistory []sdk.Message,
	priorSummary string,
	lm fantasy.LanguageModel,
	opts SpawnOpts,
	pool *AgentPool,
	toolsFn func() []fantasy.AgentTool,
	onToken func(string),
	onToolCall func(id, name, input string),
	onDone func(error),
) {
	if lm == nil {
		if onDone != nil {
			onDone(fmt.Errorf("agent %s: no language model configured", a.id))
		}
		return
	}

	// Snapshot the model name once (guarded read; SetModel may swap it between
	// turns). lm was already captured consistently with it in Submit.
	modelName := a.ModelName()

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

	// Control-only wake: empty prompt and every drained inbox message is a
	// Go-level control message (system/steering). These are filtered from LLM
	// context, so an LLM call would send an empty prompt and error out — and the
	// erroring turn would skip finishTurn's shutdown/drain handling, stranding a
	// shutdown_request delivered to an idle agent. Short-circuit straight to
	// finishTurn so the control message (e.g. shutdown_request) is acted on
	// without an LLM round-trip. See deliver_test.go shutdown-to-idle coverage.
	if content == "" && len(inboxMsgs) > 0 && allControlMessages(inboxMsgs) {
		a.finishTurn(ctx, nil, nil, onDone, inboxMsgs)
		return
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
		contextWindow = contextWindowForModel(modelName)
	}
	history := priorHistory
	didCompact := false
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
			didCompact = true
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

	// buildStream applies the before_provider_request interceptor chain (when one
	// is installed) to the outgoing messages + model, just before streaming.
	// With no interceptor it returns (history, content) unchanged so the default
	// turn path is byte-identical. With an interceptor it folds content into the
	// outgoing message list, returns the (possibly redacted) list with an empty
	// prompt, rebuilds fa/lm on a model reroute, or signals a block. Redaction is
	// send-time only — history records the original content (§ NOTES).
	buildStream := func(h []sdk.Message, c string) (msgs []sdk.Message, prompt string, blocked bool, reason string) {
		if pool == nil || !pool.hasProviderRequestInterceptor() {
			return h, c, false, ""
		}
		outgoing := h
		if c != "" {
			outgoing = append(append([]sdk.Message{}, h...), sdk.Message{Role: sdk.RoleUser, Content: c})
		}
		redacted, newModel, blk, rsn := pool.interceptProviderRequest(a.id, outgoing, modelName)
		if blk {
			return nil, "", true, rsn
		}
		if newModel != "" && newModel != modelName {
			if newLM, lerr := pool.LanguageModelForModel(childCtx, newModel); lerr == nil {
				lm = newLM
				fa = fantasy.NewAgent(lm, agentOpts...)
			}
		}
		return redacted, "", false, ""
	}

	streamMsgs, streamPrompt, blocked, blockReason := buildStream(history, content)
	if blocked {
		a.finishTurn(ctx, &ProviderRequestBlockedError{Reason: blockReason}, nil, onDone, inboxMsgs)
		return
	}

	collectedText, usage, err := streamTurn(childCtx, fa, streamMsgs, streamPrompt, pool, onToken, onToolCall)

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
		streamMsgs, streamPrompt, blocked, blockReason = buildStream(history, content)
		if blocked {
			a.finishTurn(ctx, &ProviderRequestBlockedError{Reason: blockReason}, nil, onDone, inboxMsgs)
			return
		}
		collectedText, usage, err = streamTurn(childCtx, fa, streamMsgs, streamPrompt, pool, onToken, onToolCall)
	}

	// Record token usage for the turn. On error or cancellation, store a
	// zero-valued usage so a failed turn never reports stale counts.
	if err == nil && childCtx.Err() == nil {
		a.setLastUsage(usage)
		// Forward context window usage for the main agent so the harness/status
		// bar and WASM extensions (EventContextUsage) see the latest usage.
		// Sub-agent turns do not drive the main context indicator.
		if pool != nil && a.id == MainAgentID {
			pool.dispatchContextUsage(sdk.ContextUsageFromFantasy(usage, contextWindow), didCompact)
		}
	} else {
		a.setLastUsage(fantasy.Usage{})
	}

	// Always record what the user said and the assistant response.
	// Providers require strictly alternating user/assistant messages —
	// leaving history ending with a lone user message causes "text content
	// cannot be empty" on the next turn.
	assistantText := placeholderForEmptyResponse(collectedText, childCtx.Err() != nil)
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

	a.finishTurn(ctx, err, childCtx.Err(), onDone, inboxMsgs)
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
func (a *Agent) finishTurn(ctx context.Context, err error, ctxErr error, onDone func(error), consumed []sdk.Message) {
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

		// Also scan the inbox messages that Submit consumed as THIS turn's content.
		// A shutdown_request delivered to an idle agent is drained by Submit (not by
		// the DrainInbox above) and filtered from history as a system message — so
		// without this scan it would be silently lost and the agent would never
		// self-close. Only the shutdown_request is recovered here; consumed normal
		// messages were already processed as turn content and must not be re-queued.
		if shutdownFrom == "" {
			for _, m := range consumed {
				if m.Type != sdk.MessageTypeSystem {
					continue
				}
				var evt struct {
					Event string `json:"event"`
					From  string `json:"from"`
				}
				if json.Unmarshal([]byte(m.Content), &evt) == nil &&
					evt.Event == "shutdown_request" {
					shutdownFrom = evt.From
					break
				}
			}
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

		// No normal pending messages remain. Notify the creator that this agent
		// has gone idle so the orchestrator can review results or shut it down.
		// Skip when a shutdown is pending (handled below) or when the agent has no
		// creator (top-level agents such as main never self-notify). The message is
		// a normal (model-visible) message, unlike the system-only AGENT_SHUTDOWN.
		if shutdownFrom == "" && a.creatorID != "" && a.pool != nil {
			idleMsg := fmt.Sprintf(
				"[agent '%s' is idle — review its results with get_agent_status or shut it down with shutdown_agent]",
				a.id,
			)
			if derr := a.pool.Deliver(a.creatorID, sdk.Message{
				Role:    sdk.RoleUser,
				Content: idleMsg,
			}, true); derr != nil && !errors.Is(derr, ErrAgentNotFound) {
				slog.Warn("finishTurn: failed to notify creator of idle", "agent", a.id, "creator", a.creatorID, "err", derr)
			}
		}

		// Handle deferred shutdown if present.
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

// Placeholder assistant-message text used when a turn produces no streamed text.
// Providers reject empty text content blocks and require strictly alternating
// user/assistant messages, so the assistant turn is never recorded as an empty
// string — one of these placeholders stands in instead.
const (
	// placeholderCancelled marks a turn whose context was cancelled before any
	// text was produced.
	placeholderCancelled = "[response cancelled]"
	// placeholderToolOnly marks a turn that did real work via tool calls but
	// emitted no assistant text.
	placeholderToolOnly = "[tool calls only]"
)

// placeholderForEmptyResponse returns collected unchanged when it is non-empty,
// otherwise the appropriate placeholder so the assistant turn is never recorded
// as an empty string (see the placeholder* constants). cancelled selects the
// cancellation placeholder over the tool-only one.
func placeholderForEmptyResponse(collected string, cancelled bool) string {
	if collected != "" {
		return collected
	}
	if cancelled {
		return placeholderCancelled
	}
	return placeholderToolOnly
}

// allControlMessages reports whether every message is a Go-level control
// message (system or steering) that is filtered from LLM context. Such a batch
// produces no LLM-visible content, so a turn carrying only these would send an
// empty prompt.
func allControlMessages(msgs []sdk.Message) bool {
	for _, m := range msgs {
		if m.Type != sdk.MessageTypeSystem && m.Type != sdk.MessageTypeSteering {
			return false
		}
	}
	return true
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
) (string, fantasy.Usage, error) {
	var collected string
	res, err := fa.Stream(ctx, fantasy.AgentStreamCall{
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
	var usage fantasy.Usage
	if res != nil {
		usage = res.TotalUsage
	}
	return collected, usage, err
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
