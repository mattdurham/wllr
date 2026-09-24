package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
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
	// nextID numbers IDs assigned by append to messages that arrive without
	// one. Queued-message IDs are what make ID-based delete/edit usable while
	// a turn is running (indexes shift as messages arrive; IDs do not).
	nextID int
}

// append adds msg to the queue. Messages with blank content are dropped (and
// logged against agentID) — empty content causes Anthropic API rejection
// ("text content blocks must be non-empty"). Messages without an ID get one
// assigned ("q<n>") so queue_peek can report a stable selector for
// delete/edit; a caller-supplied ID is preserved. Thread-safe.
func (b *mailbox) append(agentID string, msg sdk.Message) {
	if strings.TrimSpace(msg.Content) == "" {
		slog.Warn("agent: dropping inbox message with empty content", "agent", agentID, "role", msg.Role)
		return
	}
	b.mu.Lock()
	if msg.ID == "" {
		b.nextID++
		msg.ID = fmt.Sprintf("q%d", b.nextID)
	}
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

// clear atomically removes all queued messages and returns how many were
// removed. Unlike deleteByIndex/editByIndex it is safe to call while the
// agent's turn is running: Submit drains the inbox at turn start and the
// post-turn drain re-checks it at turn end, so a concurrent clear simply
// makes both drains observe an empty queue and cleared messages are never
// replayed into a later turn. Thread-safe.
func (b *mailbox) clear() int {
	b.mu.Lock()
	n := len(b.msgs)
	b.msgs = nil
	b.mu.Unlock()
	return n
}

// len returns the number of queued messages without draining. Thread-safe.
func (b *mailbox) len() int {
	b.mu.RLock()
	n := len(b.msgs)
	b.mu.RUnlock()
	return n
}

// snapshot returns a read-only copy of all messages. Thread-safe.
func (b *mailbox) snapshot() []sdk.Message {
	b.mu.RLock()
	defer b.mu.RUnlock()
	// Create a deep copy of the slice
	copy := make([]sdk.Message, len(b.msgs))
	for i, msg := range b.msgs {
		copy[i] = sdk.Message{
			ID:      msg.ID,
			Role:    msg.Role,
			Content: msg.Content,
			Type:    msg.Type,
		}
	}
	return copy
}

// deleteByIndex removes the message at the given index and returns it.
// Returns nil if index is out of range. Thread-safe.
func (b *mailbox) deleteByIndex(index int) *sdk.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	if index < 0 || index >= len(b.msgs) {
		return nil
	}
	msg := b.msgs[index]
	b.msgs = append(b.msgs[:index], b.msgs[index+1:]...)
	return &msg
}

// editByIndex updates the message at the given index and returns the old value.
// Returns nil if index is out of range. Thread-safe.
func (b *mailbox) editByIndex(index int, newMsg sdk.Message) *sdk.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	if index < 0 || index >= len(b.msgs) {
		return nil
	}
	old := b.msgs[index]
	if strings.TrimSpace(newMsg.Content) == "" {
		slog.Warn("agent: skipping inbox edit with empty content", "index", index)
		return nil
	}
	b.msgs[index] = newMsg
	return &old
}

// deleteByID removes the first message with the given ID and returns it.
// Returns nil if no matching message is found. Thread-safe.
func (b *mailbox) deleteByID(id string) *sdk.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, msg := range b.msgs {
		if msg.ID == id {
			b.msgs = append(b.msgs[:i], b.msgs[i+1:]...)
			return &msg
		}
	}
	return nil
}

// editByID updates the first message with the given ID and returns the old value.
// Returns nil if no matching message is found. Thread-safe.
func (b *mailbox) editByID(id string, newMsg sdk.Message) *sdk.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	if strings.TrimSpace(newMsg.Content) == "" {
		slog.Warn("agent: skipping inbox edit with empty content", "id", id)
		return nil
	}
	for i, msg := range b.msgs {
		if msg.ID == id {
			old := b.msgs[i]
			b.msgs[i] = newMsg
			return &old
		}
	}
	return nil
}
