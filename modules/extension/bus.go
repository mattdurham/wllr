package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"

	"github.com/mattdurham/wllr/modules/sdk"
)

// EventBus is a single, shared event stream. All events fired anywhere in
// wllr pass through it. Handlers registered via Subscribe are called
// asynchronously (fire-and-forget). If no handlers are registered for an
// event type, Publish is a zero-cost no-op.

// fast O(1) has-subscribers check

// NewEventBus returns an empty EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[sdk.EventType][]Handler),
		counts:   make(map[sdk.EventType]int),
	}
}

// Subscribe registers a handler for eventType. Thread-safe.
func (b *EventBus) Subscribe(eventType sdk.EventType, h Handler) {
	b.mu.Lock()
	b.handlers[eventType] = append(b.handlers[eventType], h)
	b.counts[eventType]++
	b.mu.Unlock()
}

// Unsubscribe removes all handlers for eventType. Thread-safe.
func (b *EventBus) Unsubscribe(eventType sdk.EventType) {
	b.mu.Lock()
	delete(b.handlers, eventType)
	delete(b.counts, eventType)
	b.mu.Unlock()
}

// HasSubscribers returns true if at least one handler is registered for
// eventType. O(1).
func (b *EventBus) HasSubscribers(eventType sdk.EventType) bool {
	b.mu.RLock()
	n := b.counts[eventType]
	b.mu.RUnlock()
	return n > 0
}

// Publish dispatches evt to all registered handlers asynchronously.
// If no handlers are registered for evt.Type this is a no-op.
func (b *EventBus) Publish(ctx context.Context, evt sdk.Event) {
	b.mu.RLock()
	n := b.counts[evt.Type]
	if n == 0 {
		b.mu.RUnlock()
		return
	}
	hs := make([]Handler, len(b.handlers[evt.Type]))
	copy(hs, b.handlers[evt.Type])
	b.mu.RUnlock()

	go func() {
		for _, h := range hs {
			_ = h(ctx, evt) // errors are fire-and-forget; callers may log internally
		}
	}()
}
