package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/sdk"
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
	systemPrompt string
	opts         SpawnOpts
	inbox        []sdk.Message

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

	// inbox holds messages injected between turns via AppendInbox.
	inboxMu sync.Mutex

	// cancelMu protects the cancel function for the current active turn.
	cancelMu sync.Mutex

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
// Thread-safe.
func (a *Agent) AppendInbox(msg sdk.Message) {
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

// ID returns the agent's unique identifier within its pool.
func (a *Agent) ID() string { return a.id }

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

	// Snapshot current history under lock.
	a.historyMu.Lock()
	priorHistory := make([]sdk.Message, len(a.history))
	copy(priorHistory, a.history)
	a.historyMu.Unlock()

	// Prepend inbox messages to prior history so the LLM sees them as prior context.
	if len(inboxMsgs) > 0 {
		combined := make([]sdk.Message, 0, len(inboxMsgs)+len(priorHistory))
		combined = append(combined, inboxMsgs...)
		combined = append(combined, priorHistory...)
		priorHistory = combined
	}

	// Create a child context and store its cancel.
	childCtx, cancel := context.WithCancel(ctx)
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

	pool := a.pool
	lm := a.lm
	opts := a.opts

	go func() {
		defer cancel()

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

		fa := fantasy.NewAgent(lm, agentOpts...)

		// Auto-compact: retry once with trimmed history if context is too long.
		history := priorHistory
		var collectedText string
		var err error
		for attempt := 0; attempt < 2; attempt++ {
			collectedText = ""
			_, err = fa.Stream(childCtx, fantasy.AgentStreamCall{
				Messages: sdkToFantasyMessages(history),
				Prompt:   content,
				OnTextDelta: func(id, text string) error {
					if text == "" {
						return nil
					}
					select {
					case <-childCtx.Done():
						return childCtx.Err()
					default:
					}
					collectedText += text
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

			if err != nil && isContextTooLong(err) && attempt == 0 {
				// Trim to the most recent 20 messages and retry once.
				if onToken != nil {
					onToken("\n\n[Context too long — compacting history and retrying…]\n\n")
				}
				keep := 20
				if len(history) > keep {
					history = history[len(history)-keep:]
				}
				// Also trim persisted history for future turns.
				a.historyMu.Lock()
				if len(a.history) > keep {
					a.history = a.history[len(a.history)-keep:]
				}
				a.historyMu.Unlock()
				continue
			}
			break
		}

		// Append the user message and assistant reply to history.
		a.historyMu.Lock()
		a.history = append(a.history, sdk.Message{Role: sdk.RoleUser, Content: content})
		if collectedText != "" {
			a.history = append(a.history, sdk.Message{Role: sdk.RoleAssistant, Content: collectedText})
		}
		a.historyMu.Unlock()

		if onDone != nil {
			onDone(err)
		}
	}()
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
