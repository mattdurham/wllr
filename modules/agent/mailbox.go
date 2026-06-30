package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"log/slog"
	"strings"
	"sync"

	"github.com/mattdurham/wllr/modules/sdk"
)

// mailbox is an agent's pending-message queue. It owns the message slice and the
// mutex guarding it, and enforces the empty-content invariant in one place: a
// message with blank content is never queued (empty text blocks are rejected by
// the Anthropic API). It does NOT own turn-execution state (isRunning) — that is
// the Agent's concern; the mailbox is purely the message store.
//
// The zero value is a ready-to-use empty mailbox. All methods are safe for
// concurrent use. mailbox must not be copied after first use (it holds a mutex);
// it is embedded by value in *Agent, which is always referenced by pointer.
type mailbox struct {
	msgs []sdk.Message
	mu   sync.RWMutex
}

// append adds msg to the queue. Messages with blank content are dropped (and
// logged against agentID) — empty content causes Anthropic API rejection
// ("text content blocks must be non-empty"). Thread-safe.
func (b *mailbox) append(agentID string, msg sdk.Message) {
	if strings.TrimSpace(msg.Content) == "" {
		slog.Warn("agent: dropping inbox message with empty content", "agent", agentID, "role", msg.Role)
		return
	}
	b.mu.Lock()
	b.msgs = append(b.msgs, msg)
	b.mu.Unlock()
}

// drain atomically returns all queued messages and clears the queue. Thread-safe.
func (b *mailbox) drain() []sdk.Message {
	b.mu.Lock()
	msgs := b.msgs
	b.msgs = nil
	b.mu.Unlock()
	return msgs
}

// len returns the number of queued messages without draining. Thread-safe.
func (b *mailbox) len() int {
	b.mu.RLock()
	n := len(b.msgs)
	b.mu.RUnlock()
	return n
}
