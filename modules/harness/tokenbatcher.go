package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

type tokenBatcher struct {
	lastSend time.Time
	p        *tea.Program
	// dispatch, when non-nil, is called with each flushed batch of text so the
	// batch can be forwarded to WASM extensions (EventToken). Called on the
	// agent goroutine, not the bubbletea loop.
	dispatch func(string)
	buf      strings.Builder
	mu       sync.Mutex
}
