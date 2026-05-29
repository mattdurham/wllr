package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"github.com/mattdurham/wllr/modules/sdk"
	"sync"
)

// EventBus is a single, shared event stream. All events fired anywhere in
// wllr pass through it. Handlers registered via Subscribe are called
// asynchronously (fire-and-forget). If no handlers are registered for an
// event type, Publish is a zero-cost no-op.
type EventBus struct {
	handlers map[sdk.EventType][]Handler
	counts   map[sdk.EventType]int
	mu       sync.RWMutex
}
