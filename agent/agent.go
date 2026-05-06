package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"fmt"
	"sync"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/sdk"
)

// Agent wraps a fantasy.LanguageModel with a message inbox and lifecycle management.
// Each agent maintains its own conversation history and can run one turn at a time.
type Agent struct {
	id   string
	lm   fantasy.LanguageModel
	opts SpawnOpts
	pool *AgentPool

	// inbox holds messages injected between turns via AppendInbox.
	inboxMu sync.Mutex
	inbox   []sdk.Message

	// cancelMu protects the cancel function for the current active turn.
	cancelMu sync.Mutex
	cancel   context.CancelFunc

	// history is the conversation history for this agent (all completed turns).
	historyMu sync.Mutex
	history   []sdk.Message

	// onToken is called per text delta. Set via SetOnToken before calling Submit.
	onTokenMu sync.RWMutex
	onToken   func(token string)

	// onDone is called when a turn completes (with nil or an error). Set via SetOnDone.
	onDoneMu sync.RWMutex
	onDone   func(err error)
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

		// Build fantasy agent tools from opts.
		var agentOpts []fantasy.AgentOption
		if len(opts.Tools) > 0 {
			agentOpts = append(agentOpts, fantasy.WithTools(opts.Tools...))
		}
		if opts.SystemPrompt != "" {
			agentOpts = append(agentOpts, fantasy.WithSystemPrompt(opts.SystemPrompt))
		}

		fa := fantasy.NewAgent(lm, agentOpts...)

		// Convert prior history to fantasy messages.
		fantasyMsgs := sdkToFantasyMessages(priorHistory)

		var collectedText string
		_, err := fa.Stream(childCtx, fantasy.AgentStreamCall{
			Messages: fantasyMsgs,
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
		})

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

