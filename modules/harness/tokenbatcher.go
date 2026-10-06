package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// msgSender is the subset of tea.Program the token batcher and the agent
// callback wiring need. An interface rather than *tea.Program lets tests
// capture sends headlessly without a running TUI.
type msgSender interface {
	Send(tea.Msg)
}

type tokenBatcher struct {
	lastSend time.Time
	p        msgSender
	// dispatch, when non-nil, is called with each flushed batch of text so the
	// batch can be forwarded to WASM extensions (EventToken). Called on the
	// agent goroutine, not the bubbletea loop.
	dispatch func(string)
	buf      strings.Builder
	mu       sync.Mutex
}
